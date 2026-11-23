package relation

import "fmt"

// Kind is the kind of a relation.
type Kind uint8

const (
	KindOne Kind = iota + 1
	KindMany
)

// Column is a column a relation query can select.
// Key is the select alias and the key in the JSON object. Name is the database column name.
// Ptr takes the field address on a row. When ReadJSON is nil, nested JSON is written to that address with encoding/json.
// When CastInJSON is set, a nested level writes this column as "alias"."column"::<CastInJSON>. numeric, timestamp, timestamptz, and bigint use text.
type Column struct {
	Key        string
	Name       string
	Ptr        func(row any) any
	ReadJSON   func(dst any, raw []byte) error
	CastInJSON string
}

// Through is the junction table of a many-to-many relation.
// From pairs with the relation's From: the junction-table columns that face the source table.
// To pairs with the relation's To: the junction-table columns that face the target table.
type Through struct {
	Table string
	From  []string
	To    []string
}

// Relation is one relation on a table. From and To are database column names and must have the same length.
// When both sides are empty, Define fills them from the inverse relation.
// Init runs before a many relation starts placing child rows, so it can install an empty slice. Place puts one child record on the parent row.
type Relation struct {
	Name     string
	Kind     Kind
	Target   string
	From     []string
	To       []string
	Alias    string
	Optional bool
	Where    *Filter
	// When Reversed is true, Where is written on the source table. Define sets it when borrowing Where from the inverse relation.
	Reversed bool
	Through  *Through
	Init     func(parent any)
	Place    func(parent, child any)
}

// Many declares a many relation. from and to are single database column names.
func Many(name, target, from, to string) Relation {
	return Relation{Name: name, Kind: KindMany, Target: target, From: []string{from}, To: []string{to}}
}

// One declares a one relation. optional defaults to true, as in rc.5, and only affects how the caller reads an empty result.
func One(name, target, from, to string) Relation {
	return Relation{Name: name, Kind: KindOne, Target: target, From: []string{from}, To: []string{to}, Optional: true}
}

// ManyOf declares a many relation with from and to still unset, and leaves Define to infer them from the inverse relation.
func ManyOf(name, target string) Relation {
	return Relation{Name: name, Kind: KindMany, Target: target}
}

// OneOf declares a one relation with from and to still unset.
func OneOf(name, target string) Relation {
	return Relation{Name: name, Kind: KindOne, Target: target, Optional: true}
}

// ManyThrough declares a many-to-many relation through a junction table.
// junctionFrom faces from, and junctionTo faces to.
func ManyThrough(name, target, from, to, junction, junctionFrom, junctionTo string) Relation {
	return Relation{
		Name:   name,
		Kind:   KindMany,
		Target: target,
		From:   []string{from},
		To:     []string{to},
		Through: &Through{
			Table: junction,
			From:  []string{junctionFrom},
			To:    []string{junctionTo},
		},
	}
}

// Table is one table in the schema. New allocates a row for FindMany and should return a pointer.
type Table struct {
	Name      string
	SQLName   string
	Columns   []Column
	Relations []Relation
	New       func() any

	byKey  map[string]int
	byName map[string]int
	byRel  map[string]int
}

// Schema is a set of tables and the relations among them.
type Schema struct {
	tables map[string]*Table
	order  []string
}

// Define registers every table and fills in from and to where they can be inferred.
func Define(tables []Table) (*Schema, error) {
	return define(tables, false)
}

// DefinePart registers only tables that have a relation list. A table whose Relations is nil does not enter the schema.
func DefinePart(tables []Table) (*Schema, error) {
	return define(tables, true)
}

func define(tables []Table, part bool) (*Schema, error) {
	owned := make([]Table, 0, len(tables))
	for _, t := range tables {
		if part && t.Relations == nil {
			continue
		}
		if t.Name == "" {
			return nil, fmt.Errorf("relation: table name is empty")
		}
		if t.SQLName == "" {
			t.SQLName = t.Name
		}
		t.Columns = append([]Column(nil), t.Columns...)
		if t.Relations != nil {
			t.Relations = append([]Relation(nil), t.Relations...)
		}
		t.byKey = make(map[string]int, len(t.Columns))
		t.byName = make(map[string]int, len(t.Columns))
		for i := range t.Columns {
			c := &t.Columns[i]
			if c.Key == "" {
				c.Key = c.Name
			}
			if c.Name == "" || c.Key == "" {
				return nil, fmt.Errorf("relation: table %q has a column with no name", t.Name)
			}
			if _, ok := t.byKey[c.Key]; ok {
				return nil, fmt.Errorf("relation: table %q column key %q is duplicated", t.Name, c.Key)
			}
			if _, ok := t.byName[c.Name]; ok {
				return nil, fmt.Errorf("relation: table %q column %q is duplicated", t.Name, c.Name)
			}
			t.byKey[c.Key] = i
			t.byName[c.Name] = i
		}
		t.byRel = make(map[string]int, len(t.Relations))
		for i := range t.Relations {
			r := &t.Relations[i]
			if r.Name == "" {
				return nil, fmt.Errorf("relation: table %q has a relation with no name", t.Name)
			}
			if r.Kind != KindOne && r.Kind != KindMany {
				return nil, fmt.Errorf("relation: table %q relation %q has the wrong kind", t.Name, r.Name)
			}
			if _, ok := t.byRel[r.Name]; ok {
				return nil, fmt.Errorf("relation: table %q relation %q is duplicated", t.Name, r.Name)
			}
			if _, ok := t.byKey[r.Name]; ok {
				return nil, fmt.Errorf("relation: table %q relation %q collides with a column", t.Name, r.Name)
			}
			t.byRel[r.Name] = i
		}
		owned = append(owned, t)
	}
	s := &Schema{tables: make(map[string]*Table, len(owned)), order: make([]string, len(owned))}
	for i := range owned {
		if _, ok := s.tables[owned[i].Name]; ok {
			return nil, fmt.Errorf("relation: table %q is duplicated", owned[i].Name)
		}
		s.tables[owned[i].Name] = &owned[i]
		s.order[i] = owned[i].Name
	}
	for _, name := range s.order {
		table := s.tables[name]
		for i := range table.Relations {
			if err := s.bindRelation(table, &table.Relations[i]); err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}

func (s *Schema) bindRelation(table *Table, rel *Relation) error {
	target := s.tables[rel.Target]
	if target == nil {
		return fmt.Errorf("relation: relation %q.%q target table %q is not in the schema", table.Name, rel.Name, rel.Target)
	}
	if len(rel.From) == 0 && len(rel.To) == 0 {
		rev, err := s.findReverse(table, rel)
		if err != nil {
			return err
		}
		rel.From = append([]string(nil), rev.To...)
		rel.To = append([]string(nil), rev.From...)
		if rel.Through == nil && rev.Through != nil {
			rel.Through = &Through{
				Table: rev.Through.Table,
				From:  append([]string(nil), rev.Through.To...),
				To:    append([]string(nil), rev.Through.From...),
			}
		}
		if rel.Where == nil {
			rel.Reversed = true
			rel.Where = rev.Where
		}
	} else if len(rel.From) == 0 || len(rel.To) == 0 || len(rel.From) != len(rel.To) {
		return fmt.Errorf("relation: relation %q.%q from and to must be paired and the same length", table.Name, rel.Name)
	}
	for _, name := range rel.From {
		if _, ok := table.byName[name]; !ok {
			return fmt.Errorf("relation: relation %q.%q from column %q is not on table %q", table.Name, rel.Name, name, table.Name)
		}
	}
	for _, name := range rel.To {
		if _, ok := target.byName[name]; !ok {
			return fmt.Errorf("relation: relation %q.%q to column %q is not on table %q", table.Name, rel.Name, name, target.Name)
		}
	}
	if rel.Through == nil {
		return nil
	}
	junction := s.tables[rel.Through.Table]
	if junction == nil {
		return fmt.Errorf("relation: relation %q.%q junction table %q is not in the schema", table.Name, rel.Name, rel.Through.Table)
	}
	if len(rel.Through.From) != len(rel.From) || len(rel.Through.To) != len(rel.To) {
		return fmt.Errorf("relation: relation %q.%q junction columns must match the length of from and to", table.Name, rel.Name)
	}
	for _, name := range append(append([]string(nil), rel.Through.From...), rel.Through.To...) {
		if _, ok := junction.byName[name]; !ok {
			return fmt.Errorf("relation: relation %q.%q junction column %q is not on table %q", table.Name, rel.Name, name, junction.Name)
		}
	}
	return nil
}

func (s *Schema) findReverse(src *Table, rel *Relation) (*Relation, error) {
	target := s.tables[rel.Target]
	var found *Relation
	for i := range target.Relations {
		other := &target.Relations[i]
		if other.Target != src.Name {
			continue
		}
		if rel.Alias != "" {
			if other.Alias != rel.Alias {
				continue
			}
		} else if other.Alias != "" {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("relation: relation %q.%q has multiple inverse relations; alias is required", src.Name, rel.Name)
		}
		found = other
	}
	if found == nil {
		return nil, fmt.Errorf("relation: relation %q.%q has no from/to and no inverse relation", src.Name, rel.Name)
	}
	if len(found.From) == 0 || len(found.To) == 0 {
		return nil, fmt.Errorf("relation: relation %q.%q inverse %q.%q also has no from/to", src.Name, rel.Name, target.Name, found.Name)
	}
	return found, nil
}

// Get looks up a table by its name in the schema.
func (s *Schema) Get(name string) (*Table, bool) {
	if s == nil {
		return nil, false
	}
	t, ok := s.tables[name]
	return t, ok
}

// Column looks up a column by key.
func (t *Table) Column(key string) (Column, bool) {
	if t == nil {
		return Column{}, false
	}
	i, ok := t.byKey[key]
	if !ok {
		return Column{}, false
	}
	return t.Columns[i], true
}

// Relation looks up a relation by name.
func (t *Table) Relation(name string) (Relation, bool) {
	if t == nil {
		return Relation{}, false
	}
	i, ok := t.byRel[name]
	if !ok {
		return Relation{}, false
	}
	return t.Relations[i], true
}

// Pointer returns this column's field address on the row.
func (t *Table) Pointer(key string, row any) (any, error) {
	c, ok := t.Column(key)
	if !ok {
		return nil, UnknownColumn(t.Name, key)
	}
	if c.Ptr == nil {
		return nil, fmt.Errorf("relation: column %q.%q has no field pointer", t.Name, key)
	}
	return c.Ptr(row), nil
}
