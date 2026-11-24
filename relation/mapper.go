package relation

import (
	"encoding/json"
	"fmt"
)

// Fill loads relation-column JSON into the row whose scalars are already scanned.
// relJSON lines up one-to-one with Selection.Rels. An empty array for many calls Init.
func (s *Schema) Fill(table string, sel Selection, dest any, relJSON [][]byte) error {
	tab, ok := s.Get(table)
	if !ok {
		return fmt.Errorf("relation: table %q is not in the schema", table)
	}
	return s.apply(tab, sel, dest, relJSON)
}

func (s *Schema) apply(tab *Table, sel Selection, dest any, relJSON [][]byte) error {
	if len(relJSON) != len(sel.Rels) {
		return fmt.Errorf("relation: table %q relation column count does not match", tab.Name)
	}
	for i, relSel := range sel.Rels {
		rel, ok := tab.Relation(relSel.Name)
		if !ok {
			return UnknownRelation(tab.Name, relSel.Name)
		}
		target, ok := s.Get(rel.Target)
		if !ok {
			return fmt.Errorf("relation: table %q is not in the schema", rel.Target)
		}
		raw := relJSON[i]
		if rel.Kind == KindMany {
			if rel.Init != nil {
				rel.Init(dest)
			}
			if len(raw) == 0 || string(raw) == "null" {
				continue
			}
			var arr []json.RawMessage
			if err := json.Unmarshal(raw, &arr); err != nil {
				return fmt.Errorf("relation: relation %q.%q is not a JSON array: %w", tab.Name, rel.Name, err)
			}
			for _, item := range arr {
				child, err := newRow(target)
				if err != nil {
					return err
				}
				if err := s.fillObject(target, relSel.Inner, child, item); err != nil {
					return err
				}
				if rel.Place == nil {
					return fmt.Errorf("relation: relation %q.%q has no Place", tab.Name, rel.Name)
				}
				rel.Place(dest, child)
			}
			continue
		}
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		child, err := newRow(target)
		if err != nil {
			return err
		}
		if err := s.fillObject(target, relSel.Inner, child, raw); err != nil {
			return err
		}
		if rel.Place == nil {
			return fmt.Errorf("relation: relation %q.%q has no Place", tab.Name, rel.Name)
		}
		rel.Place(dest, child)
	}
	return nil
}

func (s *Schema) fillObject(tab *Table, sel Selection, dest any, raw []byte) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("relation: table %q nested row is not a JSON object: %w", tab.Name, err)
	}
	for _, key := range sel.Columns {
		val, ok := obj[key]
		if !ok || string(val) == "null" {
			continue
		}
		col, ok := tab.Column(key)
		if !ok {
			return UnknownColumn(tab.Name, key)
		}
		if err := decodeColumn(tab.Name, col, dest, val); err != nil {
			return err
		}
	}
	for _, ex := range sel.Extras {
		val, ok := obj[ex.Key]
		if !ok || string(val) == "null" {
			continue
		}
		if err := decodeExtra(tab.Name, ex, dest, val); err != nil {
			return err
		}
	}
	nested := make([][]byte, len(sel.Rels))
	for i, relSel := range sel.Rels {
		nested[i] = []byte(obj[relSel.Name])
	}
	return s.apply(tab, sel, dest, nested)
}

func decodeColumn(table string, col Column, dest any, raw []byte) error {
	if col.Ptr == nil {
		return fmt.Errorf("relation: table %s column %s: expected a field pointer", table, col.Key)
	}
	dst := col.Ptr(dest)
	if col.ReadJSON != nil {
		if err := col.ReadJSON(dst, raw); err != nil {
			return fmt.Errorf("relation: column %q.%q cannot be read from JSON: %w", table, col.Key, err)
		}
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("relation: column %q.%q cannot be read from JSON: %w", table, col.Key, err)
	}
	return nil
}

func decodeExtra(table string, ex SelectedExtra, dest any, raw []byte) error {
	if ex.Ptr == nil {
		return fmt.Errorf("relation: table %s extras %s: expected a field pointer", table, ex.Key)
	}
	dst := ex.Ptr(dest)
	if ex.ReadJSON != nil {
		if err := ex.ReadJSON(dst, raw); err != nil {
			return fmt.Errorf("relation: extras %q cannot be read from JSON: %w", ex.Key, err)
		}
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("relation: extras %q cannot be read from JSON: %w", ex.Key, err)
	}
	return nil
}

func newRow(tab *Table) (any, error) {
	if tab.New == nil {
		return nil, fmt.Errorf("relation: table %q has no New", tab.Name)
	}
	row := tab.New()
	if row == nil {
		return nil, fmt.Errorf("relation: table %q New returned nil", tab.Name)
	}
	return row, nil
}
