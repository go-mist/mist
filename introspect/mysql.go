package introspect

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-mist/mist/schema"
)

// ReadMySQL reads tables in one database. Views are not read.
// The migration bookkeeping tables __mist_migrations and __mist_lock are skipped.
func ReadMySQL(ctx context.Context, q Querier, database string) (schema.Schema, error) {
	if q == nil {
		return schema.Schema{}, fmt.Errorf("introspect: missing connection")
	}
	if database == "" {
		return schema.Schema{}, fmt.Errorf("introspect: missing database")
	}
	tables, order, err := mysqlColumns(ctx, q, database)
	if err != nil {
		return schema.Schema{}, err
	}
	if err := mysqlPrimary(ctx, q, database, tables); err != nil {
		return schema.Schema{}, err
	}
	if err := mysqlUniques(ctx, q, database, tables); err != nil {
		return schema.Schema{}, err
	}
	if err := mysqlIndexes(ctx, q, database, tables); err != nil {
		return schema.Schema{}, err
	}
	if err := mysqlForeignKeys(ctx, q, database, tables); err != nil {
		return schema.Schema{}, err
	}
	if err := mysqlChecks(ctx, q, database, tables); err != nil {
		return schema.Schema{}, err
	}
	out := schema.Schema{}
	for _, name := range order {
		out.Tables = append(out.Tables, tables[name].Table)
	}
	return out, nil
}

func mysqlColumns(ctx context.Context, q Querier, database string) (map[string]*built, []string, error) {
	rows, err := q.Query(ctx, `
		select table_name, column_name, data_type, is_nullable, column_default, extra,
		       character_maximum_length, numeric_precision, numeric_scale
		from information_schema.columns
		where table_schema = ?
		order by table_name, ordinal_position`, database)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	tables := map[string]*built{}
	var order []string
	for rows.Next() {
		var tableName, colName, dataType, nullable, extra string
		var def *string
		var charLen, precision, scale *int64
		if err := rows.Scan(&tableName, &colName, &dataType, &nullable, &def, &extra, &charLen, &precision, &scale); err != nil {
			return nil, nil, err
		}
		if skipBookkeeping(tableName) {
			continue
		}
		table := tables[tableName]
		if table == nil {
			table = &built{Table: schema.Table{Name: tableName}}
			tables[tableName] = table
			order = append(order, tableName)
		}
		col := schema.Column{
			Name:     colName,
			Type:     mysqlType(dataType, charLen, precision, scale),
			NotNull:  strings.EqualFold(nullable, "NO"),
			Identity: strings.Contains(strings.ToLower(extra), "auto_increment"),
		}
		if def != nil {
			col.Default = strings.TrimSpace(*def)
		}
		if col.Identity {
			col.Default = ""
		}
		table.Columns = append(table.Columns, col)
	}
	return tables, order, rows.Err()
}

func mysqlType(dataType string, charLen, precision, scale *int64) schema.Type {
	name := strings.ToLower(dataType)
	typ := schema.Type{Name: name}
	switch name {
	case "varchar", "char":
		if charLen != nil {
			typ.Length = int(*charLen)
		}
	case "decimal", "numeric":
		if precision != nil {
			typ.Precision = int(*precision)
		}
		if scale != nil {
			typ.Scale = int(*scale)
		}
	}
	return typ
}

func mysqlPrimary(ctx context.Context, q Querier, database string, tables map[string]*built) error {
	rows, err := q.Query(ctx, `
		select table_name, column_name, constraint_name
		from information_schema.key_column_usage
		where table_schema = ? and constraint_name = 'PRIMARY'
		order by table_name, ordinal_position`, database)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tableName, colName, constraint string
		if err := rows.Scan(&tableName, &colName, &constraint); err != nil {
			return err
		}
		table := tables[tableName]
		if table == nil {
			continue
		}
		table.PrimaryKey = append(table.PrimaryKey, colName)
		table.PrimaryKeyName = constraint
	}
	return rows.Err()
}

func mysqlUniques(ctx context.Context, q Querier, database string, tables map[string]*built) error {
	rows, err := q.Query(ctx, `
		select tc.table_name, tc.constraint_name, kcu.column_name
		from information_schema.table_constraints tc
		join information_schema.key_column_usage kcu
		  on kcu.constraint_schema = tc.constraint_schema
		 and kcu.table_name = tc.table_name
		 and kcu.constraint_name = tc.constraint_name
		where tc.table_schema = ? and tc.constraint_type = 'UNIQUE'
		order by tc.table_name, tc.constraint_name, kcu.ordinal_position`, database)
	if err != nil {
		return err
	}
	defer rows.Close()
	type key struct{ table, name string }
	grouped := map[key]*schema.Unique{}
	var order []key
	for rows.Next() {
		var tableName, name, column string
		if err := rows.Scan(&tableName, &name, &column); err != nil {
			return err
		}
		if tables[tableName] == nil {
			continue
		}
		k := key{tableName, name}
		item := grouped[k]
		if item == nil {
			item = &schema.Unique{Name: name}
			grouped[k] = item
			order = append(order, k)
		}
		item.Columns = append(item.Columns, column)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, k := range order {
		table := tables[k.table]
		table.Uniques = append(table.Uniques, *grouped[k])
	}
	return nil
}

func mysqlIndexes(ctx context.Context, q Querier, database string, tables map[string]*built) error {
	rows, err := q.Query(ctx, `
		select table_name, index_name, non_unique, column_name
		from information_schema.statistics
		where table_schema = ? and index_name <> 'PRIMARY'
		order by table_name, index_name, seq_in_index`, database)
	if err != nil {
		return err
	}
	defer rows.Close()
	type key struct{ table, name string }
	grouped := map[key]*schema.Index{}
	var order []key
	for rows.Next() {
		var tableName, name, column string
		var nonUnique int
		if err := rows.Scan(&tableName, &name, &nonUnique, &column); err != nil {
			return err
		}
		table := tables[tableName]
		if table == nil || uniqueName(table, name) {
			continue
		}
		k := key{tableName, name}
		item := grouped[k]
		if item == nil {
			item = &schema.Index{Name: name, Unique: nonUnique == 0}
			grouped[k] = item
			order = append(order, k)
		}
		item.Columns = append(item.Columns, column)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, k := range order {
		tables[k.table].Indexes = append(tables[k.table].Indexes, *grouped[k])
	}
	return nil
}

func uniqueName(table *built, name string) bool {
	for _, unique := range table.Uniques {
		if unique.Name == name {
			return true
		}
	}
	return false
}

func mysqlForeignKeys(ctx context.Context, q Querier, database string, tables map[string]*built) error {
	rows, err := q.Query(ctx, `
		select kcu.table_name, kcu.constraint_name, kcu.column_name,
		       kcu.referenced_table_name, kcu.referenced_column_name,
		       rc.delete_rule, rc.update_rule
		from information_schema.key_column_usage kcu
		join information_schema.referential_constraints rc
		  on rc.constraint_schema = kcu.constraint_schema
		 and rc.constraint_name = kcu.constraint_name
		where kcu.table_schema = ? and kcu.referenced_table_name is not null
		order by kcu.table_name, kcu.constraint_name, kcu.ordinal_position`, database)
	if err != nil {
		return err
	}
	defer rows.Close()
	type key struct{ table, name string }
	grouped := map[key]*schema.ForeignKey{}
	var order []key
	for rows.Next() {
		var tableName, name, column, refTable, refColumn, onDelete, onUpdate string
		if err := rows.Scan(&tableName, &name, &column, &refTable, &refColumn, &onDelete, &onUpdate); err != nil {
			return err
		}
		if tables[tableName] == nil {
			continue
		}
		k := key{tableName, name}
		item := grouped[k]
		if item == nil {
			item = &schema.ForeignKey{
				Name:     name,
				RefTable: refTable,
				OnDelete: mysqlAction(onDelete),
				OnUpdate: mysqlAction(onUpdate),
			}
			grouped[k] = item
			order = append(order, k)
		}
		item.Columns = append(item.Columns, column)
		item.RefColumns = append(item.RefColumns, refColumn)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, k := range order {
		tables[k.table].ForeignKeys = append(tables[k.table].ForeignKeys, *grouped[k])
	}
	return nil
}

func mysqlAction(rule string) string {
	switch strings.ToLower(strings.TrimSpace(rule)) {
	case "", "restrict", "no action":
		return ""
	default:
		return strings.ToLower(rule)
	}
}

func mysqlChecks(ctx context.Context, q Querier, database string, tables map[string]*built) error {
	rows, err := q.Query(ctx, `
		select tc.table_name, tc.constraint_name, cc.check_clause
		from information_schema.table_constraints tc
		join information_schema.check_constraints cc
		  on cc.constraint_schema = tc.constraint_schema
		 and cc.constraint_name = tc.constraint_name
		where tc.table_schema = ? and tc.constraint_type = 'CHECK'
		order by tc.table_name, tc.constraint_name`, database)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tableName, name, expr string
		if err := rows.Scan(&tableName, &name, &expr); err != nil {
			return err
		}
		table := tables[tableName]
		if table == nil {
			continue
		}
		table.Checks = append(table.Checks, schema.Check{Name: name, Expr: strings.TrimSpace(expr)})
	}
	return rows.Err()
}

func skipBookkeeping(name string) bool {
	return name == "__mist_migrations" || name == "__mist_lock"
}
