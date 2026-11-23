// Package diff compares two schemas and produces ordered changes.
// Columns and tables are paired by name. A name that does not match is recorded as a drop followed by an add. Renames are not inferred.
package diff

import (
	"fmt"
	"strings"

	"github.com/go-mist/mist/schema"
)

// Kind is the category of one change.
type Kind string

const (
	CreateEnum      Kind = "create_enum"
	DropEnum        Kind = "drop_enum"
	AlterEnum       Kind = "alter_enum"
	CreateTable     Kind = "create_table"
	DropTable       Kind = "drop_table"
	AddColumn       Kind = "add_column"
	DropColumn      Kind = "drop_column"
	AlterColumn     Kind = "alter_column"
	AlterPrimaryKey Kind = "alter_primary_key"
	AddUnique       Kind = "add_unique"
	DropUnique      Kind = "drop_unique"
	AddIndex        Kind = "add_index"
	DropIndex       Kind = "drop_index"
	AddCheck        Kind = "add_check"
	DropCheck       Kind = "drop_check"
	AddForeignKey   Kind = "add_foreign_key"
	DropForeignKey  Kind = "drop_foreign_key"
)

// Change is one change. Table and column pointers refer to elements of the arguments. The caller must not modify that Schema further.
type Change struct {
	Kind        Kind
	Destructive bool
	Summary     string
	Loc         schema.Loc
	Table       string

	Enum       *schema.Enum
	EnumFrom   *schema.Enum
	TableDef   *schema.Table
	Column     *schema.Column
	ColumnFrom *schema.Column
	Name       string
	Unique     *schema.Unique
	Index      *schema.Index
	Check      *schema.Check
	ForeignKey *schema.ForeignKey

	PrimaryKeyFrom []string
	PrimaryKeyTo   []string
	PrimaryKeyName string
}

// Diff compares desired against actual. An empty actual is a database that has no tables yet.
func Diff(desired, actual schema.Schema) []Change {
	var out []Change
	out = append(out, diffEnums(desired.Enums, actual.Enums)...)

	actualTables := indexTables(actual.Tables)
	seen := map[string]struct{}{}
	for i := range desired.Tables {
		want := &desired.Tables[i]
		seen[want.Name] = struct{}{}
		have, ok := actualTables[want.Name]
		if !ok {
			out = append(out, Change{
				Kind:     CreateTable,
				Summary:  "create table " + want.Name,
				Loc:      want.Loc,
				Table:    want.Name,
				TableDef: want,
			})
			continue
		}
		out = append(out, diffTable(want, have)...)
	}
	for i := range actual.Tables {
		have := &actual.Tables[i]
		if _, ok := seen[have.Name]; ok {
			continue
		}
		out = append(out, Change{
			Kind:        DropTable,
			Destructive: true,
			Summary:     "drop table " + have.Name,
			Loc:         have.Loc,
			Table:       have.Name,
			TableDef:    have,
		})
	}
	return out
}

func diffEnums(desired, actual []schema.Enum) []Change {
	have := map[string]*schema.Enum{}
	for i := range actual {
		have[actual[i].Name] = &actual[i]
	}
	var out []Change
	seen := map[string]struct{}{}
	for i := range desired {
		want := &desired[i]
		seen[want.Name] = struct{}{}
		old, ok := have[want.Name]
		if !ok {
			out = append(out, Change{
				Kind:    CreateEnum,
				Summary: "create enum " + want.Name,
				Loc:     want.Loc,
				Enum:    want,
			})
			continue
		}
		if len(want.Values) > 0 && len(old.Values) > 0 && !sameStrings(want.Values, old.Values) {
			out = append(out, Change{
				Kind:     AlterEnum,
				Summary:  "enum " + want.Name + " values changed",
				Loc:      want.Loc,
				Enum:     want,
				EnumFrom: old,
			})
		}
	}
	for i := range actual {
		old := &actual[i]
		if _, ok := seen[old.Name]; ok {
			continue
		}
		out = append(out, Change{
			Kind:        DropEnum,
			Destructive: true,
			Summary:     "drop enum " + old.Name,
			Loc:         old.Loc,
			Enum:        old,
		})
	}
	return out
}

func diffTable(want, have *schema.Table) []Change {
	var out []Change
	haveCols := map[string]*schema.Column{}
	for i := range have.Columns {
		haveCols[have.Columns[i].Name] = &have.Columns[i]
	}
	seenCols := map[string]struct{}{}
	for i := range want.Columns {
		col := &want.Columns[i]
		seenCols[col.Name] = struct{}{}
		old, ok := haveCols[col.Name]
		if !ok {
			out = append(out, Change{
				Kind:    AddColumn,
				Summary: fmt.Sprintf("table %s add column %s", want.Name, col.Name),
				Loc:     col.Loc,
				Table:   want.Name,
				Column:  col,
			})
			continue
		}
		if columnChanged(old, col) {
			out = append(out, Change{
				Kind:        AlterColumn,
				Destructive: !sameType(old.Type, col.Type),
				Summary:     fmt.Sprintf("table %s column %s changed", want.Name, col.Name),
				Loc:         col.Loc,
				Table:       want.Name,
				Column:      col,
				ColumnFrom:  old,
			})
		}
	}
	for i := range have.Columns {
		old := &have.Columns[i]
		if _, ok := seenCols[old.Name]; ok {
			continue
		}
		out = append(out, Change{
			Kind:        DropColumn,
			Destructive: true,
			Summary:     fmt.Sprintf("table %s drop column %s", want.Name, old.Name),
			Loc:         old.Loc,
			Table:       want.Name,
			Name:        old.Name,
			ColumnFrom:  old,
		})
	}
	if !sameStrings(want.PrimaryKey, have.PrimaryKey) {
		out = append(out, Change{
			Kind:           AlterPrimaryKey,
			Summary:        "table " + want.Name + " primary key changed",
			Loc:            want.Loc,
			Table:          want.Name,
			PrimaryKeyFrom: append([]string(nil), have.PrimaryKey...),
			PrimaryKeyTo:   append([]string(nil), want.PrimaryKey...),
			PrimaryKeyName: schema.PrimaryKeyNameOf(want.Name, have.PrimaryKeyName),
		})
	}
	out = append(out, diffNamed(want.Name, want.Uniques, have.Uniques)...)
	out = append(out, diffIndexes(want.Name, want.Indexes, have.Indexes)...)
	out = append(out, diffChecks(want.Name, want.Checks, have.Checks)...)
	out = append(out, diffForeignKeys(want.Name, want.ForeignKeys, have.ForeignKeys)...)
	return out
}

func columnChanged(old, col *schema.Column) bool {
	return !sameType(old.Type, col.Type) ||
		old.NotNull != col.NotNull ||
		old.Default != col.Default ||
		old.Generated != col.Generated ||
		old.Identity != col.Identity
}

func diffNamed(table string, want, have []schema.Unique) []Change {
	haveBy := map[string]*schema.Unique{}
	for i := range have {
		haveBy[have[i].Name] = &have[i]
	}
	var out []Change
	seen := map[string]struct{}{}
	for i := range want {
		item := &want[i]
		seen[item.Name] = struct{}{}
		old, ok := haveBy[item.Name]
		if !ok {
			out = append(out, Change{
				Kind:    AddUnique,
				Summary: fmt.Sprintf("table %s add unique %s", table, item.Name),
				Loc:     item.Loc,
				Table:   table,
				Unique:  item,
			})
			continue
		}
		if !sameStrings(old.Columns, item.Columns) {
			out = append(out,
				Change{
					Kind:    DropUnique,
					Summary: fmt.Sprintf("table %s drop unique %s", table, old.Name),
					Loc:     old.Loc,
					Table:   table,
					Name:    old.Name,
				},
				Change{
					Kind:    AddUnique,
					Summary: fmt.Sprintf("table %s add unique %s", table, item.Name),
					Loc:     item.Loc,
					Table:   table,
					Unique:  item,
				},
			)
		}
	}
	for i := range have {
		old := &have[i]
		if _, ok := seen[old.Name]; ok {
			continue
		}
		out = append(out, Change{
			Kind:    DropUnique,
			Summary: fmt.Sprintf("table %s drop unique %s", table, old.Name),
			Loc:     old.Loc,
			Table:   table,
			Name:    old.Name,
		})
	}
	return out
}

func diffIndexes(table string, want, have []schema.Index) []Change {
	haveBy := map[string]*schema.Index{}
	for i := range have {
		haveBy[have[i].Name] = &have[i]
	}
	var out []Change
	seen := map[string]struct{}{}
	for i := range want {
		item := &want[i]
		seen[item.Name] = struct{}{}
		old, ok := haveBy[item.Name]
		if !ok {
			out = append(out, Change{
				Kind:    AddIndex,
				Summary: fmt.Sprintf("table %s add index %s", table, item.Name),
				Loc:     item.Loc,
				Table:   table,
				Index:   item,
			})
			continue
		}
		if old.Unique != item.Unique || !sameStrings(old.Columns, item.Columns) {
			out = append(out,
				Change{
					Kind:    DropIndex,
					Summary: fmt.Sprintf("table %s drop index %s", table, old.Name),
					Loc:     old.Loc,
					Table:   table,
					Name:    old.Name,
				},
				Change{
					Kind:    AddIndex,
					Summary: fmt.Sprintf("table %s add index %s", table, item.Name),
					Loc:     item.Loc,
					Table:   table,
					Index:   item,
				},
			)
		}
	}
	for i := range have {
		old := &have[i]
		if _, ok := seen[old.Name]; ok {
			continue
		}
		out = append(out, Change{
			Kind:    DropIndex,
			Summary: fmt.Sprintf("table %s drop index %s", table, old.Name),
			Loc:     old.Loc,
			Table:   table,
			Name:    old.Name,
		})
	}
	return out
}

func diffChecks(table string, want, have []schema.Check) []Change {
	haveBy := map[string]*schema.Check{}
	for i := range have {
		haveBy[have[i].Name] = &have[i]
	}
	var out []Change
	seen := map[string]struct{}{}
	for i := range want {
		item := &want[i]
		seen[item.Name] = struct{}{}
		old, ok := haveBy[item.Name]
		if !ok {
			out = append(out, Change{
				Kind:    AddCheck,
				Summary: fmt.Sprintf("table %s add CHECK %s", table, item.Name),
				Loc:     item.Loc,
				Table:   table,
				Check:   item,
			})
			continue
		}
		if old.Expr != item.Expr {
			out = append(out,
				Change{
					Kind:    DropCheck,
					Summary: fmt.Sprintf("table %s drop CHECK %s", table, old.Name),
					Loc:     old.Loc,
					Table:   table,
					Name:    old.Name,
				},
				Change{
					Kind:    AddCheck,
					Summary: fmt.Sprintf("table %s add CHECK %s", table, item.Name),
					Loc:     item.Loc,
					Table:   table,
					Check:   item,
				},
			)
		}
	}
	for i := range have {
		old := &have[i]
		if _, ok := seen[old.Name]; ok {
			continue
		}
		out = append(out, Change{
			Kind:    DropCheck,
			Summary: fmt.Sprintf("table %s drop CHECK %s", table, old.Name),
			Loc:     old.Loc,
			Table:   table,
			Name:    old.Name,
		})
	}
	return out
}

func diffForeignKeys(table string, want, have []schema.ForeignKey) []Change {
	haveBy := map[string]*schema.ForeignKey{}
	for i := range have {
		haveBy[fkKey(&have[i])] = &have[i]
	}
	var out []Change
	seen := map[string]struct{}{}
	for i := range want {
		item := &want[i]
		key := fkKey(item)
		seen[key] = struct{}{}
		if _, ok := haveBy[key]; ok {
			continue
		}
		out = append(out, Change{
			Kind:       AddForeignKey,
			Summary:    fmt.Sprintf("table %s add foreign key %s", table, item.Name),
			Loc:        item.Loc,
			Table:      table,
			ForeignKey: item,
		})
	}
	for i := range have {
		old := &have[i]
		if _, ok := seen[fkKey(old)]; ok {
			continue
		}
		out = append(out, Change{
			Kind:    DropForeignKey,
			Summary: fmt.Sprintf("table %s drop foreign key %s", table, old.Name),
			Loc:     old.Loc,
			Table:   table,
			Name:    old.Name,
		})
	}
	return out
}

func fkKey(fk *schema.ForeignKey) string {
	return strings.Join(fk.Columns, ",") + "->" + fk.RefTable + "(" + strings.Join(fk.RefColumns, ",") + ")" +
		" del=" + fk.OnDelete + " upd=" + fk.OnUpdate
}

func indexTables(tables []schema.Table) map[string]*schema.Table {
	out := make(map[string]*schema.Table, len(tables))
	for i := range tables {
		out[tables[i].Name] = &tables[i]
	}
	return out
}

func sameType(a, b schema.Type) bool {
	return a.Name == b.Name && a.Length == b.Length && a.Precision == b.Precision &&
		a.Scale == b.Scale && a.ArrayDims == b.ArrayDims && a.Enum == b.Enum
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
