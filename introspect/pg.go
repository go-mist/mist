// Package introspect reads back from PostgreSQL the objects covered by ADR-0008.
// Views, triggers, functions, privileges, extensions, and sequences are not read. Partitioned tables and expression indexes return an error, so diff does not treat them as absent.
package introspect

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/schema"
)

// Querier is a connection that can run a query. *pg.DB and a transaction both satisfy it.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (query.Rows, error)
}

// Read reads the tables, columns, constraints, indexes, and enums in one schema.
func Read(ctx context.Context, q Querier, schemaName string) (schema.Schema, error) {
	if q == nil {
		return schema.Schema{}, fmt.Errorf("introspect: missing connection")
	}
	if schemaName == "" {
		return schema.Schema{}, fmt.Errorf("introspect: missing schema")
	}
	var found int
	err := queryOne(ctx, q, &found, `select 1 from pg_namespace where nspname = $1`, schemaName)
	if err != nil {
		if errors.Is(err, errNoRow) {
			return schema.Schema{}, fmt.Errorf("introspect: schema %s does not exist", schemaName)
		}
		return schema.Schema{}, err
	}
	if err := unsupported(ctx, q, schemaName); err != nil {
		return schema.Schema{}, err
	}
	enums, err := readEnums(ctx, q, schemaName)
	if err != nil {
		return schema.Schema{}, err
	}
	order, byName, err := readColumns(ctx, q, schemaName)
	if err != nil {
		return schema.Schema{}, err
	}
	if err := readConstraints(ctx, q, schemaName, byName); err != nil {
		return schema.Schema{}, err
	}
	if err := readIndexes(ctx, q, schemaName, byName); err != nil {
		return schema.Schema{}, err
	}
	out := schema.Schema{Enums: enums}
	for _, name := range order {
		out.Tables = append(out.Tables, byName[name].Table)
	}
	return out, nil
}

type built struct {
	schema.Table
}

func unsupported(ctx context.Context, q Querier, schemaName string) error {
	rows, err := q.Query(ctx, `
		select c.relname, c.relkind::text
		from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = $1 and c.relkind in ('p', 'f')
		order by c.relname
		limit 1`, schemaName)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		return rows.Err()
	}
	var name, kind string
	if err := rows.Scan(&name, &kind); err != nil {
		return err
	}
	what := "partitioned table"
	if kind == "f" {
		what = "external table"
	}
	return fmt.Errorf("introspect: %s %s is not supported yet", what, name)
}

func readEnums(ctx context.Context, q Querier, schemaName string) ([]schema.Enum, error) {
	rows, err := q.Query(ctx, `
		select t.typname, e.enumlabel
		from pg_type t
		join pg_namespace n on n.oid = t.typnamespace
		join pg_enum e on e.enumtypid = t.oid
		where n.nspname = $1 and t.typtype = 'e'
		order by t.typname, e.enumsortorder`, schemaName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []schema.Enum
	for rows.Next() {
		var name, label string
		if err := rows.Scan(&name, &label); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].Name != name {
			out = append(out, schema.Enum{Name: name})
		}
		out[len(out)-1].Values = append(out[len(out)-1].Values, label)
	}
	return out, rows.Err()
}

func readColumns(ctx context.Context, q Querier, schemaName string) ([]string, map[string]*built, error) {
	rows, err := q.Query(ctx, `
		select c.relname, a.attname, a.attnotnull,
		       a.attidentity::text, a.attgenerated::text, a.atttypmod, a.attndims,
		       ty.typname, ty.typtype::text, ty.typcategory::text, tn.nspname,
		       coalesce(et.typname, ''), coalesce(et.typtype::text, ''), coalesce(en.nspname, ''),
		       pg_get_expr(ad.adbin, ad.adrelid)
		from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		join pg_attribute a on a.attrelid = c.oid and a.attnum > 0 and not a.attisdropped
		join pg_type ty on ty.oid = a.atttypid
		join pg_namespace tn on tn.oid = ty.typnamespace
		left join pg_type et on et.oid = ty.typelem
		left join pg_namespace en on en.oid = et.typnamespace
		left join pg_attrdef ad on ad.adrelid = a.attrelid and ad.adnum = a.attnum
		where n.nspname = $1 and c.relkind = 'r'
		order by c.relname, a.attnum`, schemaName)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var order []string
	byName := map[string]*built{}
	for rows.Next() {
		var tableName, colName, identity, generated, typeName, typeKind, category, typeSchema string
		var elemName, elemKind, elemSchema string
		var notNull bool
		var typmod int32
		var ndims int16
		var expr *string
		if err := rows.Scan(
			&tableName, &colName, &notNull,
			&identity, &generated, &typmod, &ndims,
			&typeName, &typeKind, &category, &typeSchema,
			&elemName, &elemKind, &elemSchema, &expr,
		); err != nil {
			return nil, nil, err
		}
		table := byName[tableName]
		if table == nil {
			table = &built{Table: schema.Table{Name: tableName}}
			byName[tableName] = table
			order = append(order, tableName)
		}
		typ, err := canonicalType(typeFact{
			Name: typeName, TypeKind: typeKind, Category: category, Mod: int(typmod), Dims: int(ndims),
			TableSchema: schemaName, TypeSchema: typeSchema,
			ElemName: elemName, ElemKind: elemKind, ElemSchema: elemSchema,
		}, tableName+"."+colName)
		if err != nil {
			return nil, nil, err
		}
		col := schema.Column{Name: colName, Type: typ, NotNull: notNull}
		if expr != nil {
			switch {
			case generated == "s":
				col.Generated = *expr
			case identity == "a" || identity == "d":
				col.Identity = true
			default:
				col.Default = *expr
			}
		}
		if identity == "a" || identity == "d" {
			col.Identity = true
		}
		foldColumn(&col)
		table.Columns = append(table.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return order, byName, nil
}

func readConstraints(ctx context.Context, q Querier, schemaName string, tables map[string]*built) error {
	rows, err := q.Query(ctx, `
		select rel.relname, con.conname, con.contype::text,
		       coalesce(pg_get_expr(con.conbin, con.conrelid), ''),
		       coalesce(ref.relname, ''),
		       coalesce(con.confdeltype::text, ''),
		       coalesce(con.confupdtype::text, ''),
		       coalesce((
		           select array_agg(att.attname order by cols.ord)
		           from unnest(con.conkey) with ordinality as cols(attnum, ord)
		           join pg_attribute att on att.attrelid = con.conrelid and att.attnum = cols.attnum
		       ), array[]::text[]),
		       coalesce((
		           select array_agg(att.attname order by cols.ord)
		           from unnest(con.confkey) with ordinality as cols(attnum, ord)
		           join pg_attribute att on att.attrelid = con.confrelid and att.attnum = cols.attnum
		       ), array[]::text[])
		from pg_constraint con
		join pg_class rel on rel.oid = con.conrelid
		join pg_namespace n on n.oid = rel.relnamespace
		left join pg_class ref on ref.oid = con.confrelid
		where n.nspname = $1 and rel.relkind = 'r' and con.contype in ('p', 'u', 'f', 'c')
		order by rel.relname, con.conname`, schemaName)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tableName, conName, conType, checkExpr, refTable, onDelete, onUpdate string
		var columns, refColumns []string
		if err := rows.Scan(&tableName, &conName, &conType, &checkExpr, &refTable, &onDelete, &onUpdate, &columns, &refColumns); err != nil {
			return err
		}
		table := tables[tableName]
		if table == nil {
			return fmt.Errorf("introspect: constraint %s: table %s was not found", conName, tableName)
		}
		switch conType {
		case "p":
			if len(table.PrimaryKey) > 0 {
				return fmt.Errorf("introspect: table %s has more than one primary key", tableName)
			}
			table.PrimaryKey = columns
			table.PrimaryKeyName = conName
		case "u":
			table.Uniques = append(table.Uniques, schema.Unique{Name: conName, Columns: columns})
		case "c":
			table.Checks = append(table.Checks, schema.Check{Name: conName, Expr: unwrapExpr(checkExpr)})
		case "f":
			del, err := fkAction(onDelete)
			if err != nil {
				return err
			}
			upd, err := fkAction(onUpdate)
			if err != nil {
				return err
			}
			table.ForeignKeys = append(table.ForeignKeys, schema.ForeignKey{
				Name: conName, Columns: columns, RefTable: refTable, RefColumns: refColumns,
				OnDelete: del, OnUpdate: upd,
			})
		default:
			return fmt.Errorf("introspect: unknown constraint %s", conType)
		}
	}
	return rows.Err()
}

func readIndexes(ctx context.Context, q Querier, schemaName string, tables map[string]*built) error {
	rows, err := q.Query(ctx, `
		select rel.relname, ic.relname, ix.indisunique,
		       (ix.indexprs is not null or ix.indpred is not null) as expr,
		       coalesce((
		           select array_agg(att.attname order by cols.ord)
		           from unnest(ix.indkey) with ordinality as cols(attnum, ord)
		           join pg_attribute att on att.attrelid = rel.oid and att.attnum = cols.attnum
		           where cols.attnum > 0
		       ), array[]::text[])
		from pg_index ix
		join pg_class rel on rel.oid = ix.indrelid
		join pg_class ic on ic.oid = ix.indexrelid
		join pg_namespace n on n.oid = rel.relnamespace
		where n.nspname = $1 and rel.relkind = 'r' and not ix.indisprimary
		  and not exists (select 1 from pg_constraint c where c.conindid = ix.indexrelid)
		order by rel.relname, ic.relname`, schemaName)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tableName, indexName string
		var unique, expr bool
		var columns []string
		if err := rows.Scan(&tableName, &indexName, &unique, &expr, &columns); err != nil {
			return err
		}
		if expr || len(columns) == 0 {
			return fmt.Errorf("introspect: index %s has an expression or predicate, which is not supported yet", indexName)
		}
		table := tables[tableName]
		if table == nil {
			return fmt.Errorf("introspect: index %s: table %s was not found", indexName, tableName)
		}
		table.Indexes = append(table.Indexes, schema.Index{Name: indexName, Columns: columns, Unique: unique})
	}
	return rows.Err()
}

var errNoRow = errors.New("no row")

func queryOne(ctx context.Context, q Querier, dest any, sql string, args ...any) error {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return errNoRow
	}
	if dest == nil {
		return rows.Err()
	}
	if err := rows.Scan(dest); err != nil {
		return err
	}
	return rows.Err()
}
