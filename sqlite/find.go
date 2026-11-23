package sqlite

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xbmlz/mist/query"
	"github.com/xbmlz/mist/relation"
	"github.com/xbmlz/mist/sql"
)

// Queryer is a connection that can run one query. Both sqlite.DB and sqlite.Tx satisfy it.
type Queryer interface {
	Query(ctx context.Context, sql string, args ...any) (query.Rows, error)
}

// FindMany executes findMany and loads nested JSON into T.
// New for this table in the schema must return *T.
func FindMany[T any](ctx context.Context, db Queryer, schema *relation.Schema, table string, cfg relation.Config) ([]T, error) {
	return find[T](ctx, db, schema, table, cfg, relation.ModeMany)
}

// FindFirst executes findFirst. It returns query.ErrNoRows when there is no row.
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
				return nil, fmt.Errorf("relation: table %q extras %q has no field pointer", table, ex.Key)
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

func relationalCacheKey(rendered sql.Query) (string, error) {
	args, err := json.Marshal(rendered.Params)
	if err != nil {
		return "", err
	}
	return rendered.SQL + "\n" + string(args), nil
}
