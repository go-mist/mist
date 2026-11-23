package relation

import "fmt"

// UnknownRelation reports that with, where, or orderBy named a relation that was not declared.
func UnknownRelation(table, name string) error {
	return fmt.Errorf("relation: unknown relation %q -> %q", table, name)
}

// UnknownColumn reports that the column key is not on this table.
func UnknownColumn(table, key string) error {
	return fmt.Errorf("relation: unknown column %q.%q", table, key)
}

// NoFields reports that this level has no columns, no extras, and no relations.
func NoFields(table string) error {
	return fmt.Errorf("relation: table %q selected no columns", table)
}

// UnknownFilter reports that the filter key is neither a column nor a relation.
func UnknownFilter(table, key string) error {
	return fmt.Errorf("relation: unknown filter field %q.%q", table, key)
}
