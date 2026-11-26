package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/relation"
	"github.com/go-mist/mist/sql"
)

// Queryer is a connection that can run one query. Both mysql.DB and mysql.Tx satisfy it.
type Queryer interface {
	Query(ctx context.Context, sql string, args ...any) (query.Rows, error)
}

// FindMany runs findMany and loads nested JSON into T.
// New for this table in the schema must return *T.
func FindMany[T any](ctx context.Context, db Queryer, schema *relation.Schema, table string, cfg relation.Config) ([]T, error) {
	return find[T](ctx, db, schema, table, cfg, relation.ModeMany)
}

// FindFirst runs findFirst. It returns query.ErrNoRows when there is no row.
func FindFirst[T any](ctx context.Context, db Queryer, schema *relation.Schema, table string, cfg relation.Config) (T, error) {
	var zero T
	rows, err := find[T](ctx, db, schema, table, cfg, relation.ModeFirst)
	if err != nil {
		return zero, err
	}
	if len(rows) == 0 {
		return zero, query.ErrNoRows
	}
	if len(rows) > 1 {
		return zero, query.ErrManyRows
	}
	return rows[0], nil
}

func find[T any](ctx context.Context, db Queryer, schema *relation.Schema, table string, cfg relation.Config, mode string) ([]T, error) {
	if db == nil {
		return nil, fmt.Errorf("relation: missing connection")
	}
	if cfg.WithCache != nil && *cfg.WithCache && cfg.Cache == nil {
		return nil, fmt.Errorf("relation: WithCache needs Cache")
	}
	built, err := BuildRelationalQuery(schema, table, cfg, mode)
	if err != nil {
		return nil, err
	}
	tab, ok := schema.Get(table)
	if !ok {
		return nil, fmt.Errorf("relation: table %q is not in the schema", table)
	}
	rendered, err := Dialect{}.SQLToQuery(built.Query)
	if err != nil {
		return nil, err
	}
	useCache := cfg.WithCache != nil && *cfg.WithCache
	var key string
	if useCache {
		key, err = relationalCacheKey(rendered)
		if err != nil {
			return nil, err
		}
		blob, ok, err := cfg.Cache.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		if ok {
			var hit []T
			if err := json.Unmarshal(blob, &hit); err != nil {
				return nil, err
			}
			return hit, nil
		}
	}
	rows, err := db.Query(ctx, rendered.SQL, rendered.Params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		created := tab.New()
		if created == nil {
			return nil, fmt.Errorf("relation: table %q has no New", table)
		}
		row, ok := created.(*T)
		if !ok {
			var zero T
			return nil, fmt.Errorf("relation: table %q New must return *%T", table, zero)
		}
		dest := make([]any, 0, len(built.Selection.Columns)+len(built.Selection.Extras)+len(built.Selection.Rels))
		for _, key := range built.Selection.Columns {
			ptr, err := tab.Pointer(key, row)
			if err != nil {
				return nil, err
			}
			dest = append(dest, ptr)
		}
		for _, ex := range built.Selection.Extras {
			if ex.Ptr == nil {
				return nil, fmt.Errorf("relation: table %s extras %s: expected a field pointer", table, ex.Key)
			}
			dest = append(dest, ex.Ptr(row))
		}
		blobs := make([][]byte, len(built.Selection.Rels))
		for i := range blobs {
			dest = append(dest, &blobs[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		owned := make([][]byte, len(blobs))
		for i, b := range blobs {
			if b != nil {
				owned[i] = append([]byte(nil), b...)
			}
		}
		if err := sortRelationBlobs(owned, built.Selection.Rels); err != nil {
			return nil, err
		}
		if err := schema.Fill(table, built.Selection, row, owned); err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if useCache {
		blob, err := json.Marshal(out)
		if err != nil {
			return nil, err
		}
		if err := cfg.Cache.Set(ctx, key, blob); err != nil {
			return out, err
		}
	}
	return out, nil
}

// sortRelationBlobs orders many-relation arrays by mfkord.
// MariaDB's json_arrayagg does not keep the query's ORDER BY. MySQL usually does.
// The number is removed before the row is decoded, unless the selection already uses that key.
func sortRelationBlobs(blobs [][]byte, rels []relation.RelSelection) error {
	for i, rel := range rels {
		if len(blobs[i]) == 0 || string(blobs[i]) == "null" {
			continue
		}
		var next []byte
		var err error
		if rel.Many {
			next, err = sortManyJSON(blobs[i], rel.Inner)
		} else {
			next, err = sortOneJSON(blobs[i], rel.Inner)
		}
		if err != nil {
			return err
		}
		blobs[i] = next
	}
	return nil
}

func sortManyJSON(raw []byte, inner relation.Selection) ([]byte, error) {
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, err
	}
	for i := range arr {
		if err := sortNested(arr[i], inner); err != nil {
			return nil, err
		}
	}
	if !selectionUses(inner, orderCol) && arraysCarryOrder(arr) {
		sort.SliceStable(arr, func(i, j int) bool {
			return orderValue(arr[i][orderCol]) < orderValue(arr[j][orderCol])
		})
		for i := range arr {
			delete(arr[i], orderCol)
		}
	}
	return json.Marshal(arr)
}

func sortOneJSON(raw []byte, inner relation.Selection) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	if err := sortNested(obj, inner); err != nil {
		return nil, err
	}
	return json.Marshal(obj)
}

func sortNested(obj map[string]json.RawMessage, inner relation.Selection) error {
	for _, rel := range inner.Rels {
		raw, ok := obj[rel.Name]
		if !ok || len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var next []byte
		var err error
		if rel.Many {
			next, err = sortManyJSON(raw, rel.Inner)
		} else {
			next, err = sortOneJSON(raw, rel.Inner)
		}
		if err != nil {
			return err
		}
		obj[rel.Name] = next
	}
	return nil
}

func arraysCarryOrder(arr []map[string]json.RawMessage) bool {
	for _, obj := range arr {
		if _, ok := obj[orderCol]; ok {
			return true
		}
	}
	return false
}

func selectionUses(sel relation.Selection, key string) bool {
	for _, col := range sel.Columns {
		if col == key {
			return true
		}
	}
	for _, ex := range sel.Extras {
		if ex.Key == key {
			return true
		}
	}
	for _, rel := range sel.Rels {
		if rel.Name == key {
			return true
		}
	}
	return false
}

func orderValue(raw json.RawMessage) int64 {
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0
	}
	return n
}

func relationalCacheKey(rendered sql.Query) (string, error) {
	args, err := json.Marshal(rendered.Params)
	if err != nil {
		return "", err
	}
	return rendered.SQL + "\n" + string(args), nil
}
