package history

import (
	"fmt"
	"sort"
	"strings"

	"github.com/go-mist/mist/schema"
)

type present[T any] struct {
	ok bool
	v  T
}

func mergeSchemas(base schema.Schema, sides []schema.Schema, heads []string) (schema.Schema, error) {
	sideEnums := make([][]schema.Enum, len(sides))
	sideTables := make([][]schema.Table, len(sides))
	for i := range sides {
		sideEnums[i] = sides[i].Enums
		sideTables[i] = sides[i].Tables
	}
	enums, clashes := mergeEnums(base.Enums, sideEnums)
	tables, more := mergeTables(base.Tables, sideTables)
	clashes = append(clashes, more...)
	if len(clashes) > 0 {
		sort.Strings(clashes)
		return schema.Schema{}, fmt.Errorf("ddl: migrations %s both change %s", strings.Join(heads, ", "), strings.Join(clashes, ", "))
	}
	return schema.Schema{Enums: enums, Tables: tables}, nil
}

func mergeEnums(base []schema.Enum, sides [][]schema.Enum) ([]schema.Enum, []string) {
	baseBy := map[string]schema.Enum{}
	for _, item := range base {
		baseBy[item.Name] = item
	}
	sideBy := make([]map[string]schema.Enum, len(sides))
	for i, list := range sides {
		sideBy[i] = map[string]schema.Enum{}
		for _, item := range list {
			sideBy[i][item.Name] = item
		}
	}
	var out []schema.Enum
	var clashes []string
	for _, name := range orderedKeys(base, sides, func(item schema.Enum) string { return item.Name }) {
		baseV, baseOK := baseBy[name]
		pres := make([]present[schema.Enum], len(sides))
		for i := range sides {
			v, ok := sideBy[i][name]
			pres[i] = present[schema.Enum]{ok: ok, v: v}
		}
		ok, v, clash := pick(baseOK, baseV, pres, sameEnum)
		if clash {
			clashes = append(clashes, "enum "+name)
			continue
		}
		if ok {
			v.Values = cloneStrings(v.Values)
			out = append(out, v)
		}
	}
	return out, clashes
}

func mergeTables(base []schema.Table, sides [][]schema.Table) ([]schema.Table, []string) {
	baseBy := map[string]*schema.Table{}
	for i := range base {
		baseBy[base[i].Name] = &base[i]
	}
	sideBy := make([]map[string]*schema.Table, len(sides))
	for i, list := range sides {
		sideBy[i] = map[string]*schema.Table{}
		for j := range list {
			sideBy[i][list[j].Name] = &list[j]
		}
	}
	var out []schema.Table
	var clashes []string
	for _, name := range orderedKeys(base, sides, func(item schema.Table) string { return item.Name }) {
		var versions []*schema.Table
		for i := range sides {
			versions = append(versions, sideBy[i][name])
		}
		table, more := mergeTable(baseBy[name], versions)
		clashes = append(clashes, more...)
		if table != nil {
			out = append(out, *table)
		}
	}
	return out, clashes
}

func mergeTable(base *schema.Table, sides []*schema.Table) (*schema.Table, []string) {
	deleted := false
	modified := false
	for _, side := range sides {
		if side == nil && base != nil {
			deleted = true
		}
		if side != nil && base != nil && !sameTable(base, side) {
			modified = true
		}
	}
	name := ""
	if base != nil {
		name = base.Name
	} else {
		for _, side := range sides {
			if side != nil {
				name = side.Name
				break
			}
		}
	}
	if deleted && modified {
		return nil, []string{"table " + name}
	}
	if deleted {
		return nil, nil
	}
	return mergeTableBody(name, base, sides)
}

func mergeTableBody(name string, base *schema.Table, sides []*schema.Table) (*schema.Table, []string) {
	var clashes []string
	baseCols, sideCols := columnsOf(base), columnsOfAll(sides)
	cols, more := mergeColumns(name, baseCols, sideCols)
	clashes = append(clashes, more...)
	pk, pkName, more := mergePrimaryKey(name, base, sides)
	clashes = append(clashes, more...)
	uniques, more := mergeUniques(name, listOf(base, func(t *schema.Table) []schema.Unique { return t.Uniques }), listsOf(sides, func(t *schema.Table) []schema.Unique { return t.Uniques }))
	clashes = append(clashes, more...)
	indexes, more := mergeIndexes(name, listOf(base, func(t *schema.Table) []schema.Index { return t.Indexes }), listsOf(sides, func(t *schema.Table) []schema.Index { return t.Indexes }))
	clashes = append(clashes, more...)
	checks, more := mergeChecks(name, listOf(base, func(t *schema.Table) []schema.Check { return t.Checks }), listsOf(sides, func(t *schema.Table) []schema.Check { return t.Checks }))
	clashes = append(clashes, more...)
	fks, more := mergeForeignKeys(name, listOf(base, func(t *schema.Table) []schema.ForeignKey { return t.ForeignKeys }), listsOf(sides, func(t *schema.Table) []schema.ForeignKey { return t.ForeignKeys }))
	clashes = append(clashes, more...)
	if len(clashes) > 0 {
		return nil, clashes
	}
	loc := schema.Loc{}
	if base != nil {
		loc = base.Loc
	} else {
		for _, side := range sides {
			if side != nil {
				loc = side.Loc
				break
			}
		}
	}
	return &schema.Table{
		Name:           name,
		Columns:        cols,
		PrimaryKey:     pk,
		PrimaryKeyName: pkName,
		Uniques:        uniques,
		Indexes:        indexes,
		Checks:         checks,
		ForeignKeys:    fks,
		Loc:            loc,
	}, nil
}

func mergeColumns(table string, base []schema.Column, sides [][]schema.Column) ([]schema.Column, []string) {
	return mergeNamed(base, sides, func(item schema.Column) string { return item.Name }, sameColumn, func(name string) string {
		return fmt.Sprintf("table %s column %s", table, name)
	}, func(item schema.Column) schema.Column { return item })
}

func mergePrimaryKey(table string, base *schema.Table, sides []*schema.Table) ([]string, string, []string) {
	type pk struct {
		cols []string
		name string
	}
	baseOK := base != nil && len(base.PrimaryKey) > 0
	var baseV pk
	if baseOK {
		baseV = pk{cols: base.PrimaryKey, name: base.PrimaryKeyName}
	}
	pres := make([]present[pk], len(sides))
	for i, side := range sides {
		if side == nil || len(side.PrimaryKey) == 0 {
			continue
		}
		pres[i] = present[pk]{ok: true, v: pk{cols: side.PrimaryKey, name: side.PrimaryKeyName}}
	}
	ok, v, clash := pick(baseOK, baseV, pres, func(a, b pk) bool {
		return a.name == b.name && sameStrings(a.cols, b.cols)
	})
	if clash {
		return nil, "", []string{"table " + table + " primary key"}
	}
	if !ok {
		return nil, "", nil
	}
	return cloneStrings(v.cols), v.name, nil
}

func mergeUniques(table string, base []schema.Unique, sides [][]schema.Unique) ([]schema.Unique, []string) {
	return mergeNamed(base, sides, func(item schema.Unique) string { return item.Name }, sameUnique, func(name string) string {
		return fmt.Sprintf("table %s unique %s", table, name)
	}, func(item schema.Unique) schema.Unique {
		item.Columns = cloneStrings(item.Columns)
		return item
	})
}

func mergeIndexes(table string, base []schema.Index, sides [][]schema.Index) ([]schema.Index, []string) {
	return mergeNamed(base, sides, func(item schema.Index) string { return item.Name }, sameIndex, func(name string) string {
		return fmt.Sprintf("table %s index %s", table, name)
	}, func(item schema.Index) schema.Index {
		item.Columns = cloneStrings(item.Columns)
		return item
	})
}

func mergeChecks(table string, base []schema.Check, sides [][]schema.Check) ([]schema.Check, []string) {
	return mergeNamed(base, sides, func(item schema.Check) string { return item.Name }, sameCheck, func(name string) string {
		return fmt.Sprintf("table %s check %s", table, name)
	}, func(item schema.Check) schema.Check { return item })
}

func mergeForeignKeys(table string, base []schema.ForeignKey, sides [][]schema.ForeignKey) ([]schema.ForeignKey, []string) {
	return mergeNamed(base, sides, func(item schema.ForeignKey) string { return item.Name }, sameForeignKey, func(name string) string {
		return fmt.Sprintf("table %s foreign key %s", table, name)
	}, func(item schema.ForeignKey) schema.ForeignKey {
		item.Columns = cloneStrings(item.Columns)
		item.RefColumns = cloneStrings(item.RefColumns)
		return item
	})
}

func mergeNamed[T any](base []T, sides [][]T, key func(T) string, eq func(a, b T) bool, clashText func(string) string, keep func(T) T) ([]T, []string) {
	baseBy := map[string]T{}
	for _, item := range base {
		baseBy[key(item)] = item
	}
	sideBy := make([]map[string]T, len(sides))
	for i, list := range sides {
		sideBy[i] = map[string]T{}
		for _, item := range list {
			sideBy[i][key(item)] = item
		}
	}
	var out []T
	var clashes []string
	for _, name := range orderedKeys(base, sides, key) {
		baseV, baseOK := baseBy[name]
		pres := make([]present[T], len(sides))
		for i := range sides {
			v, ok := sideBy[i][name]
			pres[i] = present[T]{ok: ok, v: v}
		}
		ok, v, clash := pick(baseOK, baseV, pres, eq)
		if clash {
			clashes = append(clashes, clashText(name))
			continue
		}
		if ok {
			out = append(out, keep(v))
		}
	}
	return out, clashes
}

func pick[T any](baseOK bool, base T, sides []present[T], eq func(a, b T) bool) (bool, T, bool) {
	var zero T
	var chosen present[T]
	have := false
	for _, side := range sides {
		unchanged := side.ok == baseOK && (!side.ok || eq(side.v, base))
		if unchanged {
			continue
		}
		if !have {
			chosen = side
			have = true
			continue
		}
		if chosen.ok != side.ok || (side.ok && !eq(chosen.v, side.v)) {
			return false, zero, true
		}
	}
	if !have {
		if !baseOK {
			return false, zero, false
		}
		return true, base, false
	}
	if !chosen.ok {
		return false, zero, false
	}
	return true, chosen.v, false
}

func columnsOf(table *schema.Table) []schema.Column {
	if table == nil {
		return nil
	}
	return table.Columns
}

func columnsOfAll(sides []*schema.Table) [][]schema.Column {
	out := make([][]schema.Column, len(sides))
	for i, side := range sides {
		out[i] = columnsOf(side)
	}
	return out
}

func listOf[T any](table *schema.Table, get func(*schema.Table) []T) []T {
	if table == nil {
		return nil
	}
	return get(table)
}

func listsOf[T any](sides []*schema.Table, get func(*schema.Table) []T) [][]T {
	out := make([][]T, len(sides))
	for i, side := range sides {
		out[i] = listOf(side, get)
	}
	return out
}

func orderedKeys[T any](base []T, sides [][]T, key func(T) string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(items []T) {
		for _, item := range items {
			k := key(item)
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	add(base)
	for _, side := range sides {
		add(side)
	}
	return out
}

func sameTable(a, b *schema.Table) bool {
	if a.Name != b.Name || a.PrimaryKeyName != b.PrimaryKeyName || !sameStrings(a.PrimaryKey, b.PrimaryKey) {
		return false
	}
	if !sameNamed(a.Columns, b.Columns, func(item schema.Column) string { return item.Name }, sameColumn) {
		return false
	}
	if !sameNamed(a.Uniques, b.Uniques, func(item schema.Unique) string { return item.Name }, sameUnique) {
		return false
	}
	if !sameNamed(a.Indexes, b.Indexes, func(item schema.Index) string { return item.Name }, sameIndex) {
		return false
	}
	if !sameNamed(a.Checks, b.Checks, func(item schema.Check) string { return item.Name }, sameCheck) {
		return false
	}
	return sameNamed(a.ForeignKeys, b.ForeignKeys, func(item schema.ForeignKey) string { return item.Name }, sameForeignKey)
}

func sameNamed[T any](a, b []T, key func(T) string, eq func(T, T) bool) bool {
	if len(a) != len(b) {
		return false
	}
	by := map[string]T{}
	for _, item := range b {
		by[key(item)] = item
	}
	for _, item := range a {
		other, ok := by[key(item)]
		if !ok || !eq(item, other) {
			return false
		}
	}
	return true
}

func sameEnum(a, b schema.Enum) bool {
	return a.Name == b.Name && sameStrings(a.Values, b.Values)
}

func sameColumn(a, b schema.Column) bool {
	return a.Name == b.Name && a.NotNull == b.NotNull && a.Default == b.Default && a.Generated == b.Generated && a.Identity == b.Identity && sameType(a.Type, b.Type)
}

func sameUnique(a, b schema.Unique) bool {
	return a.Name == b.Name && sameStrings(a.Columns, b.Columns)
}

func sameIndex(a, b schema.Index) bool {
	return a.Name == b.Name && a.Unique == b.Unique && sameStrings(a.Columns, b.Columns)
}

func sameCheck(a, b schema.Check) bool {
	return a.Name == b.Name && a.Expr == b.Expr
}

func sameForeignKey(a, b schema.ForeignKey) bool {
	return a.Name == b.Name && a.RefTable == b.RefTable && a.OnDelete == b.OnDelete && a.OnUpdate == b.OnUpdate && sameStrings(a.Columns, b.Columns) && sameStrings(a.RefColumns, b.RefColumns)
}

func sameType(a, b schema.Type) bool {
	return a.Name == b.Name && a.Length == b.Length && a.Precision == b.Precision && a.Scale == b.Scale && a.ArrayDims == b.ArrayDims && a.Enum == b.Enum
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

func cloneStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return append([]string(nil), s...)
}
