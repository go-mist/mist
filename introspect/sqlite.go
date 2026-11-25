package introspect

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-mist/mist/dialect/sqlite"
	"github.com/go-mist/mist/schema"
)

// ReadSQLite reads tables in the current database file.
// The migration bookkeeping tables __drizzle_migrations and __drizzle_lock are skipped.
// A CHECK or an expression index returns an error, so diff does not treat it as absent.
func ReadSQLite(ctx context.Context, q Querier) (schema.Schema, error) {
	if q == nil {
		return schema.Schema{}, fmt.Errorf("introspect: missing connection")
	}
	rows, err := q.Query(ctx, `select name, sql from sqlite_master where type = 'table' and name not like 'sqlite_%' order by name`)
	if err != nil {
		return schema.Schema{}, err
	}
	defer rows.Close()
	var names []string
	sqlByName := map[string]string{}
	for rows.Next() {
		var name, create string
		if err := rows.Scan(&name, &create); err != nil {
			return schema.Schema{}, err
		}
		if skipBookkeeping(name) {
			continue
		}
		if strings.Contains(strings.ToLower(create), " check") {
			return schema.Schema{}, fmt.Errorf("introspect: table %s has a check constraint, which sqlite read-back does not parse", name)
		}
		names = append(names, name)
		sqlByName[name] = create
	}
	if err := rows.Err(); err != nil {
		return schema.Schema{}, err
	}
	out := schema.Schema{}
	for _, name := range names {
		table, err := sqliteTable(ctx, q, name)
		if err != nil {
			return schema.Schema{}, err
		}
		out.Tables = append(out.Tables, table)
		_ = sqlByName[name]
	}
	return out, nil
}

func sqliteTable(ctx context.Context, q Querier, name string) (schema.Table, error) {
	quoted := sqlite.Dialect{}.EscapeName(name)
	rows, err := q.Query(ctx, "pragma table_info("+quoted+")")
	if err != nil {
		return schema.Table{}, err
	}
	defer rows.Close()
	table := schema.Table{Name: name}
	for rows.Next() {
		var cid int
		var colName, colType string
		var notNull, pk int
		var def *string
		if err := rows.Scan(&cid, &colName, &colType, &notNull, &def, &pk); err != nil {
			return schema.Table{}, err
		}
		col := schema.Column{Name: colName, Type: sqliteType(colType), NotNull: notNull == 1}
		if def != nil {
			col.Default = strings.TrimSpace(*def)
		}
		table.Columns = append(table.Columns, col)
		if pk > 0 {
			for len(table.PrimaryKey) < pk {
				table.PrimaryKey = append(table.PrimaryKey, "")
			}
			table.PrimaryKey[pk-1] = colName
		}
	}
	if err := rows.Err(); err != nil {
		return schema.Table{}, err
	}
	if err := sqliteIndexes(ctx, q, &table); err != nil {
		return schema.Table{}, err
	}
	if err := sqliteForeignKeys(ctx, q, &table); err != nil {
		return schema.Table{}, err
	}
	return table, nil
}

func sqliteType(raw string) schema.Type {
	raw = strings.TrimSpace(raw)
	name := strings.ToLower(raw)
	if base, rest, ok := strings.Cut(name, "("); ok && strings.HasSuffix(rest, ")") {
		inner := strings.TrimSuffix(rest, ")")
		if strings.Contains(inner, ",") {
			parts := strings.SplitN(inner, ",", 2)
			p, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			s, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err1 == nil && err2 == nil {
				return schema.Type{Name: base, Precision: p, Scale: s}
			}
		}
		n, err := strconv.Atoi(strings.TrimSpace(inner))
		if err == nil {
			return schema.Type{Name: base, Length: n}
		}
	}
	return schema.Type{Name: name}
}

func sqliteIndexes(ctx context.Context, q Querier, table *schema.Table) error {
	quoted := sqlite.Dialect{}.EscapeName(table.Name)
	rows, err := q.Query(ctx, "pragma index_list("+quoted+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	type listed struct {
		name   string
		unique bool
		origin string
	}
	var list []listed
	for rows.Next() {
		var seq int
		var name, origin string
		var unique int
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return err
		}
		if partial != 0 {
			return fmt.Errorf("introspect: index %s has a predicate, which is not supported yet", name)
		}
		list = append(list, listed{name: name, unique: unique == 1, origin: origin})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range list {
		cols, err := sqliteIndexColumns(ctx, q, item.name)
		if err != nil {
			return err
		}
		switch item.origin {
		case "pk":
			continue
		case "u":
			table.Uniques = append(table.Uniques, schema.Unique{Name: item.name, Columns: cols})
		default:
			table.Indexes = append(table.Indexes, schema.Index{Name: item.name, Columns: cols, Unique: item.unique})
		}
	}
	return nil
}

func sqliteIndexColumns(ctx context.Context, q Querier, name string) ([]string, error) {
	rows, err := q.Query(ctx, "pragma index_info("+sqlite.Dialect{}.EscapeName(name)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var seq, cid int
		var col *string
		if err := rows.Scan(&seq, &cid, &col); err != nil {
			return nil, err
		}
		if col == nil || cid < 0 {
			return nil, fmt.Errorf("introspect: index %s has an expression, which is not supported yet", name)
		}
		cols = append(cols, *col)
	}
	return cols, rows.Err()
}

func sqliteForeignKeys(ctx context.Context, q Querier, table *schema.Table) error {
	rows, err := q.Query(ctx, "pragma foreign_key_list("+sqlite.Dialect{}.EscapeName(table.Name)+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	grouped := map[int]*schema.ForeignKey{}
	var order []int
	for rows.Next() {
		var id, seq int
		var refTable, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return err
		}
		item := grouped[id]
		if item == nil {
			item = &schema.ForeignKey{
				Name:     table.Name + "_fk_" + strconv.Itoa(id),
				RefTable: refTable,
				OnDelete: sqliteAction(onDelete),
				OnUpdate: sqliteAction(onUpdate),
			}
			grouped[id] = item
			order = append(order, id)
		}
		item.Columns = append(item.Columns, from)
		item.RefColumns = append(item.RefColumns, to)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range order {
		table.ForeignKeys = append(table.ForeignKeys, *grouped[id])
	}
	return nil
}

func sqliteAction(rule string) string {
	switch strings.ToLower(strings.TrimSpace(rule)) {
	case "", "no action", "restrict":
		return ""
	default:
		return strings.ToLower(rule)
	}
}
