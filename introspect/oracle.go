package introspect

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-mist/mist/schema"
)

// ReadOracle reads tables owned by owner. An empty owner is the current schema.
// The migration bookkeeping tables __drizzle_migrations and __drizzle_lock are skipped.
func ReadOracle(ctx context.Context, q Querier, owner string) (schema.Schema, error) {
	if q == nil {
		return schema.Schema{}, fmt.Errorf("introspect: missing connection")
	}
	if owner == "" {
		var current string
		if err := queryOne(ctx, q, &current, `select sys_context('USERENV','CURRENT_SCHEMA') from dual`); err != nil {
			return schema.Schema{}, err
		}
		owner = current
	}
	rows, err := q.Query(ctx, `select table_name from all_tables where owner = :1 order by table_name`, owner)
	if err != nil {
		return schema.Schema{}, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return schema.Schema{}, err
		}
		if skipBookkeeping(name) {
			continue
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return schema.Schema{}, err
	}
	out := schema.Schema{}
	for _, name := range names {
		table, err := oracleTable(ctx, q, owner, name)
		if err != nil {
			return schema.Schema{}, err
		}
		out.Tables = append(out.Tables, table)
	}
	return out, nil
}

func oracleTable(ctx context.Context, q Querier, owner, name string) (schema.Table, error) {
	rows, err := q.Query(ctx, `
		select column_name, data_type, data_precision, data_scale, char_length, nullable, data_default, identity_column
		from all_tab_columns
		where owner = :1 and table_name = :2
		order by column_id`, owner, name)
	if err != nil {
		return schema.Table{}, err
	}
	defer rows.Close()
	table := schema.Table{Name: name}
	for rows.Next() {
		var colName, dataType, nullable, identity string
		var precision, scale, charLen *int64
		var def *string
		if err := rows.Scan(&colName, &dataType, &precision, &scale, &charLen, &nullable, &def, &identity); err != nil {
			return schema.Table{}, err
		}
		col := schema.Column{
			Name:     colName,
			Type:     oracleType(dataType, precision, scale, charLen),
			NotNull:  strings.EqualFold(nullable, "N"),
			Identity: strings.EqualFold(identity, "YES"),
		}
		if def != nil && !col.Identity {
			col.Default = strings.TrimSpace(*def)
		}
		table.Columns = append(table.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return schema.Table{}, err
	}
	if err := oracleConstraints(ctx, q, owner, &table); err != nil {
		return schema.Table{}, err
	}
	return table, nil
}

func oracleType(dataType string, precision, scale, charLen *int64) schema.Type {
	switch strings.ToUpper(dataType) {
	case "NUMBER":
		p, s := 0, 0
		if precision != nil {
			p = int(*precision)
		}
		if scale != nil {
			s = int(*scale)
		}
		switch {
		case p == 19 && s == 0:
			return schema.Type{Name: "bigint"}
		case p == 10 && s == 0:
			return schema.Type{Name: "integer"}
		case p == 5 && s == 0:
			return schema.Type{Name: "smallint"}
		case p == 1 && s == 0:
			return schema.Type{Name: "boolean"}
		case p > 0:
			return schema.Type{Name: "numeric", Precision: p, Scale: s}
		default:
			return schema.Type{Name: "numeric"}
		}
	case "VARCHAR2":
		n := 0
		if charLen != nil {
			n = int(*charLen)
		}
		if n == 4000 {
			return schema.Type{Name: "text"}
		}
		return schema.Type{Name: "varchar", Length: n}
	case "TIMESTAMP":
		return schema.Type{Name: "timestamp"}
	case "TIMESTAMP WITH TIME ZONE":
		return schema.Type{Name: "timestamptz"}
	case "BLOB":
		return schema.Type{Name: "bytea"}
	case "JSON":
		return schema.Type{Name: "json"}
	default:
		return schema.Type{Name: strings.ToLower(dataType)}
	}
}

func oracleConstraints(ctx context.Context, q Querier, owner string, table *schema.Table) error {
	rows, err := q.Query(ctx, `
		select constraint_name, constraint_type, search_condition, r_constraint_name, delete_rule, generated, index_name
		from all_constraints
		where owner = :1 and table_name = :2
		  and constraint_type in ('P', 'U', 'R', 'C')
		order by constraint_name`, owner, table.Name)
	if err != nil {
		return err
	}
	defer rows.Close()
	type cons struct {
		name, kind, search, ref, deleteRule, generated, index string
	}
	var list []cons
	usedIndex := map[string]bool{}
	for rows.Next() {
		var item cons
		var search, ref, deleteRule, index *string
		if err := rows.Scan(&item.name, &item.kind, &search, &ref, &deleteRule, &item.generated, &index); err != nil {
			return err
		}
		if search != nil {
			item.search = *search
		}
		if ref != nil {
			item.ref = *ref
		}
		if deleteRule != nil {
			item.deleteRule = *deleteRule
		}
		if index != nil {
			item.index = *index
			usedIndex[*index] = true
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range list {
		cols, err := oracleConsColumns(ctx, q, owner, table.Name, item.name)
		if err != nil {
			return err
		}
		switch item.kind {
		case "P":
			table.PrimaryKey = cols
			table.PrimaryKeyName = item.name
		case "U":
			table.Uniques = append(table.Uniques, schema.Unique{Name: item.name, Columns: cols})
		case "R":
			refTable, refCols, err := oracleRef(ctx, q, owner, item.ref)
			if err != nil {
				return err
			}
			fk := schema.ForeignKey{Name: item.name, Columns: cols, RefTable: refTable, RefColumns: refCols}
			if !strings.EqualFold(item.deleteRule, "NO ACTION") && item.deleteRule != "" {
				fk.OnDelete = strings.ToLower(item.deleteRule)
			}
			table.ForeignKeys = append(table.ForeignKeys, fk)
		case "C":
			if strings.EqualFold(item.generated, "GENERATED NAME") {
				continue
			}
			table.Checks = append(table.Checks, schema.Check{Name: item.name, Expr: strings.TrimSpace(item.search)})
		}
	}
	return oracleIndexes(ctx, q, owner, table, usedIndex)
}

func oracleConsColumns(ctx context.Context, q Querier, owner, table, constraint string) ([]string, error) {
	rows, err := q.Query(ctx, `
		select column_name from all_cons_columns
		where owner = :1 and table_name = :2 and constraint_name = :3
		order by position`, owner, table, constraint)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

func oracleRef(ctx context.Context, q Querier, owner, constraint string) (string, []string, error) {
	var table string
	if err := queryOne(ctx, q, &table, `select table_name from all_constraints where owner = :1 and constraint_name = :2`, owner, constraint); err != nil {
		return "", nil, err
	}
	cols, err := oracleConsColumns(ctx, q, owner, table, constraint)
	return table, cols, err
}

func oracleIndexes(ctx context.Context, q Querier, owner string, table *schema.Table, used map[string]bool) error {
	rows, err := q.Query(ctx, `
		select index_name, uniqueness from all_indexes
		where table_owner = :1 and table_name = :2
		order by index_name`, owner, table.Name)
	if err != nil {
		return err
	}
	defer rows.Close()
	type listed struct {
		name   string
		unique bool
	}
	var list []listed
	for rows.Next() {
		var name, uniqueness string
		if err := rows.Scan(&name, &uniqueness); err != nil {
			return err
		}
		if used[name] {
			continue
		}
		list = append(list, listed{name: name, unique: strings.EqualFold(uniqueness, "UNIQUE")})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range list {
		cols, err := oracleIndexColumns(ctx, q, owner, table.Name, item.name)
		if err != nil {
			return err
		}
		table.Indexes = append(table.Indexes, schema.Index{Name: item.name, Columns: cols, Unique: item.unique})
	}
	return nil
}

func oracleIndexColumns(ctx context.Context, q Querier, owner, table, index string) ([]string, error) {
	rows, err := q.Query(ctx, `
		select column_name from all_ind_columns
		where table_owner = :1 and table_name = :2 and index_name = :3
		order by column_position`, owner, table, index)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var name *string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if name == nil {
			return nil, fmt.Errorf("introspect: index %s has an expression, which is not supported yet", index)
		}
		cols = append(cols, *name)
	}
	return cols, rows.Err()
}
