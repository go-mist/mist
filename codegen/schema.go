package codegen

import (
	"github.com/go-mist/mist/schema"
)

// LoadSchema reads the DDL schema from structs in a directory. It does not run user code.
func LoadSchema(dir string) (schema.Schema, error) {
	g, err := load(dir)
	if err != nil {
		return schema.Schema{}, err
	}
	if err := g.validate(); err != nil {
		return schema.Schema{}, err
	}
	return toSchema(g.tables), nil
}

func toSchema(tables []table) schema.Schema {
	var out schema.Schema
	enumAt := map[string]schema.Loc{}
	var enumOrder []string
	for _, t := range tables {
		inPK := map[string]struct{}{}
		for _, name := range t.primary {
			inPK[name] = struct{}{}
		}
		st := schema.Table{
			Name:       t.sqlName,
			PrimaryKey: append([]string(nil), t.primary...),
			Loc:        schema.Loc{File: t.file, Line: t.line},
		}
		for _, c := range t.cols {
			_, pk := inPK[c.colName]
			col := schema.Column{
				Name:      c.colName,
				NotNull:   c.notNull || c.pk || c.identity || pk,
				Default:   c.def,
				Generated: c.generated,
				Identity:  c.identity,
				Loc:       schema.Loc{File: c.file, Line: c.line},
			}
			base, dims := splitArray(c.sqlType)
			if c.enumName != "" {
				col.Type = schema.Type{Name: c.enumName, Enum: true, ArrayDims: dims}
				if _, ok := enumAt[c.enumName]; !ok {
					enumAt[c.enumName] = col.Loc
					enumOrder = append(enumOrder, c.enumName)
				}
			} else {
				col.Type = schema.Type{
					Name:      base,
					Length:    c.length,
					Precision: c.precision,
					Scale:     c.scale,
					ArrayDims: dims,
				}
			}
			st.Columns = append(st.Columns, col)
			if c.unique {
				st.Uniques = append(st.Uniques, schema.Unique{
					Name:    schema.UniqueName(t.sqlName, c.colName, c.uniqueName),
					Columns: []string{c.colName},
					Loc:     col.Loc,
				})
			}
			if c.refTable != "" {
				st.ForeignKeys = append(st.ForeignKeys, schema.ForeignKey{
					Name:       schema.ForeignKeyName(t.sqlName, []string{c.colName}, ""),
					Columns:    []string{c.colName},
					RefTable:   c.refTable,
					RefColumns: []string{c.refCol},
					OnDelete:   c.onDelete,
					OnUpdate:   c.onUpdate,
					Loc:        col.Loc,
				})
			}
		}
		for _, idx := range t.indexes {
			st.Indexes = append(st.Indexes, schema.Index{
				Name:    idx.name,
				Columns: append([]string(nil), idx.columns...),
				Unique:  idx.unique,
				Loc:     schema.Loc{File: t.file, Line: idx.line},
			})
		}
		for _, chk := range t.checks {
			st.Checks = append(st.Checks, schema.Check{
				Name: chk.name,
				Expr: chk.expr,
				Loc:  schema.Loc{File: t.file, Line: chk.line},
			})
		}
		out.Tables = append(out.Tables, st)
	}
	for _, name := range enumOrder {
		out.Enums = append(out.Enums, schema.Enum{Name: name, Loc: enumAt[name]})
	}
	return out
}
