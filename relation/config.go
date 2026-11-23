package relation

import (
	"fmt"

	"github.com/xbmlz/mist/sql"
)

// Column selection. nil means every column. Only keeps the listed keys, and that order is the select order.
// Except drops the listed keys, following column order on the table.
const (
	colAll = iota
	colOnly
	colExcept
)

// Columns decides which columns this level selects.
type Columns struct {
	mode int
	keys []string
}

// Only selects these columns, in argument order. An empty list selects no columns.
func Only(keys ...string) *Columns {
	return &Columns{mode: colOnly, keys: append([]string(nil), keys...)}
}

// Except removes these columns. The rest keep their order on the table.
func Except(keys ...string) *Columns {
	return &Columns{mode: colExcept, keys: append([]string(nil), keys...)}
}

// Selected returns the columns this level projects for the given selection. A nil set means every column.
func (t *Table) Selected(set *Columns) ([]Column, error) {
	if t == nil {
		return nil, fmt.Errorf("relation: missing table")
	}
	if set == nil || set.mode == colAll {
		out := make([]Column, len(t.Columns))
		copy(out, t.Columns)
		return out, nil
	}
	switch set.mode {
	case colOnly:
		out := make([]Column, 0, len(set.keys))
		seen := make(map[string]bool, len(set.keys))
		for _, key := range set.keys {
			if seen[key] {
				return nil, fmt.Errorf("relation: table %q column %q was selected twice", t.Name, key)
			}
			seen[key] = true
			col, ok := t.Column(key)
			if !ok {
				return nil, UnknownColumn(t.Name, key)
			}
			out = append(out, col)
		}
		return out, nil
	case colExcept:
		skip := make(map[string]bool, len(set.keys))
		for _, key := range set.keys {
			if _, ok := t.Column(key); !ok {
				return nil, UnknownColumn(t.Name, key)
			}
			skip[key] = true
		}
		var out []Column
		for _, col := range t.Columns {
			if !skip[col.Key] {
				out = append(out, col)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("relation: table %q column selection is invalid", t.Name)
	}
}

// With is a relation to include at this level. An empty Query is rc.5's `true`: every column, no filter.
type With struct {
	Name  string
	Query *Config
}

// Extra is an extra expression in the select list, rendered as `(expr) as "key"`.
// The expression is written as given. This level's table aliases are d0 and d1, increasing with depth.
// Ptr takes the field address on a row. FindMany scans with it; a nested level then decodes with ReadJSON.
type Extra struct {
	Key      string
	Expr     sql.Chunk
	Ptr      func(row any) any
	ReadJSON func(dst any, raw []byte) error
}

// Config is one level of a relation query, corresponding to rc.5's DBQueryConfig.
// Field order is fixed with slices, and maps are not used.
type Config struct {
	Columns     *Columns
	Where       Filter
	Order       Order
	Limit       *int
	Offset      *int
	With        []With
	Strategy    string
	Extras      []Extra
	Comment     string
	CommentTags map[string]string
	// When WithCache is true, Cache is used. A missing Cache returns an error.
	// nil and false do not cache. This cache does not invalidate itself.
	WithCache *bool
	Cache     Cache
}

// Bool returns a pointer to v, for setting WithCache.
func Bool(v bool) *bool { return &v }

// Int returns a pointer to n, for setting Limit and Offset. 0 is a valid value.
func Int(n int) *int { return &n }

// Selection is the columns and relations this level actually selected, used when mapping results.
type Selection struct {
	Columns []string
	Extras  []SelectedExtra
	Rels    []RelSelection
}

// SelectedExtra is an expression column in the select list.
type SelectedExtra struct {
	Key      string
	Ptr      func(row any) any
	ReadJSON func(dst any, raw []byte) error
}

// RelSelection is a relation field in the select list.
type RelSelection struct {
	Name   string
	Target string
	Many   bool
	Inner  Selection
}

// Built is the compiled result. Query is not bound to a dialect yet, and becomes text when rendered.
// WithCache reflects only the root config. The same field on a nested level does not take part in caching.
type Built struct {
	Query     *sql.SQL
	Table     string
	Selection Selection
	WithCache *bool
}
