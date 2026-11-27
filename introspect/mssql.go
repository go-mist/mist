package introspect

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-mist/mist/schema"
)

// ReadMSSQL reads tables in one schema. Views are not read.
// The migration bookkeeping tables __mist_migrations and __mist_lock are skipped.
// nvarchar(max) with a check isjson(column) = 1 is folded back to json.
func ReadMSSQL(ctx context.Context, q Querier, schemaName string) (schema.Schema, error) {
	if q == nil {
		return schema.Schema{}, fmt.Errorf("introspect: missing connection")
	}
	if schemaName == "" {
		return schema.Schema{}, fmt.Errorf("introspect: missing schema")
	}
	tables, order, err := mssqlColumns(ctx, q, schemaName)
	if err != nil {
		return schema.Schema{}, err
	}
	if err := mssqlPrimary(ctx, q, schemaName, tables); err != nil {
		return schema.Schema{}, err
	}
	if err := mssqlIndexes(ctx, q, schemaName, tables); err != nil {
		return schema.Schema{}, err
	}
	if err := mssqlForeignKeys(ctx, q, schemaName, tables); err != nil {
		return schema.Schema{}, err
	}
	if err := mssqlChecks(ctx, q, schemaName, tables); err != nil {
		return schema.Schema{}, err
	}
	out := schema.Schema{}
	for _, name := range order {
		out.Tables = append(out.Tables, tables[name].Table)
	}
	return out, nil
}

func mssqlColumns(ctx context.Context, q Querier, schemaName string) (map[string]*built, []string, error) {
	rows, err := q.Query(ctx, `
		select t.name, c.name, ty.name, c.max_length, c.precision, c.scale, c.is_nullable, c.is_identity
		from sys.tables t
		join sys.schemas s on s.schema_id = t.schema_id
		join sys.columns c on c.object_id = t.object_id
		join sys.types ty on ty.user_type_id = c.user_type_id
		where s.name = @p1
		order by t.name, c.column_id`, schemaName)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	tables := map[string]*built{}
	var order []string
	for rows.Next() {
		var tableName, colName, dataType string
		var length, precision, scale int64
		var nullable, identity bool
		if err := rows.Scan(&tableName, &colName, &dataType, &length, &precision, &scale, &nullable, &identity); err != nil {
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
		table.Columns = append(table.Columns, schema.Column{
			Name:     colName,
			Type:     mssqlType(dataType, length, precision, scale),
			NotNull:  !nullable || identity,
			Identity: identity,
		})
	}
	return tables, order, rows.Err()
}

func mssqlType(dataType string, length, precision, scale int64) schema.Type {
	switch strings.ToLower(dataType) {
	case "bit":
		return schema.Type{Name: "boolean"}
	case "uniqueidentifier":
		return schema.Type{Name: "uuid"}
	case "datetimeoffset":
		return schema.Type{Name: "timestamptz"}
	case "datetime2", "datetime":
		return schema.Type{Name: "timestamp"}
	case "varbinary":
		return schema.Type{Name: "bytea"}
	case "nvarchar", "nchar", "varchar", "char":
		if length < 0 {
			return schema.Type{Name: "text"}
		}
		n := int(length)
		if dataType == "nvarchar" || dataType == "nchar" {
			n /= 2
		}
		name := "varchar"
		if strings.HasPrefix(strings.ToLower(dataType), "n") {
			name = "varchar"
		}
		return schema.Type{Name: name, Length: n}
	case "int":
		return schema.Type{Name: "integer"}
	case "bigint":
		return schema.Type{Name: "bigint"}
	case "smallint":
		return schema.Type{Name: "smallint"}
	case "decimal", "numeric":
		return schema.Type{Name: "decimal", Precision: int(precision), Scale: int(scale)}
	case "float":
		return schema.Type{Name: "float"}
	default:
		return schema.Type{Name: strings.ToLower(dataType)}
	}
}

func mssqlPrimary(ctx context.Context, q Querier, schemaName string, tables map[string]*built) error {
	rows, err := q.Query(ctx, `
		select t.name, i.name, c.name
		from sys.indexes i
		join sys.tables t on t.object_id = i.object_id
		join sys.schemas s on s.schema_id = t.schema_id
		join sys.index_columns ic on ic.object_id = i.object_id and ic.index_id = i.index_id
		join sys.columns c on c.object_id = ic.object_id and c.column_id = ic.column_id
		where s.name = @p1 and i.is_primary_key = 1
		order by t.name, ic.key_ordinal`, schemaName)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tableName, indexName, colName string
		if err := rows.Scan(&tableName, &indexName, &colName); err != nil {
			return err
		}
		table := tables[tableName]
		if table == nil {
			continue
		}
		table.PrimaryKey = append(table.PrimaryKey, colName)
		table.PrimaryKeyName = indexName
	}
	return rows.Err()
}

func mssqlIndexes(ctx context.Context, q Querier, schemaName string, tables map[string]*built) error {
	rows, err := q.Query(ctx, `
		select t.name, i.name, c.name, i.is_unique, i.is_unique_constraint
		from sys.indexes i
		join sys.tables t on t.object_id = i.object_id
		join sys.schemas s on s.schema_id = t.schema_id
		join sys.index_columns ic on ic.object_id = i.object_id and ic.index_id = i.index_id
		join sys.columns c on c.object_id = ic.object_id and c.column_id = ic.column_id
		where s.name = @p1 and i.is_primary_key = 0 and i.is_hypothetical = 0 and ic.is_included_column = 0
		order by t.name, i.name, ic.key_ordinal`, schemaName)
	if err != nil {
		return err
	}
	defer rows.Close()
	type acc struct {
		unique     bool
		constraint bool
		cols       []string
	}
	grouped := map[string]*acc{}
	var order []string
	for rows.Next() {
		var tableName, indexName, colName string
		var unique, constraint bool
		if err := rows.Scan(&tableName, &indexName, &colName, &unique, &constraint); err != nil {
			return err
		}
		if tables[tableName] == nil {
			continue
		}
		key := tableName + "\x00" + indexName
		item := grouped[key]
		if item == nil {
			item = &acc{unique: unique, constraint: constraint}
			grouped[key] = item
			order = append(order, key)
		}
		item.cols = append(item.cols, colName)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, key := range order {
		tableName, indexName, _ := strings.Cut(key, "\x00")
		item := grouped[key]
		table := tables[tableName]
		if item.constraint {
			table.Uniques = append(table.Uniques, schema.Unique{Name: indexName, Columns: item.cols})
			continue
		}
		table.Indexes = append(table.Indexes, schema.Index{Name: indexName, Columns: item.cols, Unique: item.unique})
	}
	return nil
}

func mssqlForeignKeys(ctx context.Context, q Querier, schemaName string, tables map[string]*built) error {
	rows, err := q.Query(ctx, `
		select fk.name, tp.name, cp.name, tr.name, cr.name, fk.delete_referential_action_desc, fk.update_referential_action_desc
		from sys.foreign_keys fk
		join sys.foreign_key_columns fkc on fkc.constraint_object_id = fk.object_id
		join sys.tables tp on tp.object_id = fk.parent_object_id
		join sys.schemas s on s.schema_id = tp.schema_id
		join sys.columns cp on cp.object_id = tp.object_id and cp.column_id = fkc.parent_column_id
		join sys.tables tr on tr.object_id = fk.referenced_object_id
		join sys.columns cr on cr.object_id = tr.object_id and cr.column_id = fkc.referenced_column_id
		where s.name = @p1
		order by tp.name, fk.name, fkc.constraint_column_id`, schemaName)
	if err != nil {
		return err
	}
	defer rows.Close()
	grouped := map[string]*schema.ForeignKey{}
	var order []string
	for rows.Next() {
		var name, tableName, col, refTable, refCol, onDelete, onUpdate string
		if err := rows.Scan(&name, &tableName, &col, &refTable, &refCol, &onDelete, &onUpdate); err != nil {
			return err
		}
		if tables[tableName] == nil {
			continue
		}
		key := tableName + "\x00" + name
		item := grouped[key]
		if item == nil {
			item = &schema.ForeignKey{
				Name:     name,
				RefTable: refTable,
				OnDelete: mssqlAction(onDelete),
				OnUpdate: mssqlAction(onUpdate),
			}
			grouped[key] = item
			order = append(order, key)
		}
		item.Columns = append(item.Columns, col)
		item.RefColumns = append(item.RefColumns, refCol)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, key := range order {
		tableName, _, _ := strings.Cut(key, "\x00")
		tables[tableName].ForeignKeys = append(tables[tableName].ForeignKeys, *grouped[key])
	}
	return nil
}

func mssqlAction(rule string) string {
	switch strings.ToLower(strings.TrimSpace(rule)) {
	case "", "no_action":
		return ""
	default:
		return strings.ToLower(rule)
	}
}

func mssqlChecks(ctx context.Context, q Querier, schemaName string, tables map[string]*built) error {
	rows, err := q.Query(ctx, `
		select t.name, cc.name, cc.definition
		from sys.check_constraints cc
		join sys.tables t on t.object_id = cc.parent_object_id
		join sys.schemas s on s.schema_id = t.schema_id
		where s.name = @p1
		order by t.name, cc.name`, schemaName)
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
		if col, ok := mssqlJSONColumn(expr, table.Columns); ok {
			for i := range table.Columns {
				if table.Columns[i].Name == col && table.Columns[i].Type.Name == "text" {
					table.Columns[i].Type = schema.Type{Name: "json"}
				}
			}
			continue
		}
		table.Checks = append(table.Checks, schema.Check{Name: name, Expr: strings.TrimSpace(expr)})
	}
	return rows.Err()
}

// mssqlJSONColumn reports the column named by a check of the form isjson(column) = 1.
func mssqlJSONColumn(expr string, cols []schema.Column) (string, bool) {
	flat := strings.ToLower(expr)
	flat = strings.ReplaceAll(flat, " ", "")
	flat = strings.ReplaceAll(flat, "[", "")
	flat = strings.ReplaceAll(flat, "]", "")
	flat = strings.ReplaceAll(flat, "(", "")
	flat = strings.ReplaceAll(flat, ")", "")
	for _, col := range cols {
		if flat == "isjson"+strings.ToLower(col.Name)+"=1" {
			return col.Name, true
		}
	}
	return "", false
}
