package dm

import (
	"fmt"

	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/relation"
	"github.com/go-mist/mist/sql"
)

func (q *rqb) compileFilter(table *relation.Table, alias string, f relation.Filter, depth int) (*sql.SQL, bool, error) {
	switch f.Kind {
	case relation.FilterNone:
		return nil, false, nil
	case relation.FilterField:
		ch, err := fieldFilter(table, alias, f)
		return ch, false, err
	case relation.FilterAnd, relation.FilterOr:
		if len(f.Kids) == 0 {
			return nil, false, fmt.Errorf("relation: %s cannot be empty", filterWord(f.Kind))
		}
		parts := make([]sql.Chunk, 0, len(f.Kids))
		has := false
		for _, kid := range f.Kids {
			ch, kidHas, err := q.compileFilter(table, alias, kid, depth)
			if err != nil {
				return nil, false, err
			}
			if ch != nil {
				parts = append(parts, ch)
			}
			has = has || kidHas
		}
		if f.Kind == relation.FilterOr {
			return sql.Or(parts...), has, nil
		}
		return sql.And(parts...), has, nil
	case relation.FilterNot:
		if len(f.Kids) != 1 {
			return nil, false, fmt.Errorf("relation: NOT accepts only one condition")
		}
		inner, has, err := q.compileFilter(table, alias, f.Kids[0], depth)
		if err != nil || inner == nil {
			return nil, has, err
		}
		return sql.Not(inner), has, nil
	case relation.FilterRaw:
		if f.Raw == nil {
			return nil, false, fmt.Errorf("relation: RAW is empty")
		}
		return sql.S(f.Raw), true, nil
	case relation.FilterRel:
		return q.relFilter(table, alias, f, depth)
	default:
		return nil, false, fmt.Errorf("relation: unknown filter kind %d", f.Kind)
	}
}

func filterWord(kind int) string {
	if kind == relation.FilterOr {
		return "OR"
	}
	return "AND"
}

func fieldFilter(table *relation.Table, alias string, f relation.Filter) (*sql.SQL, error) {
	col, ok := table.Column(f.Column)
	if !ok {
		return nil, relation.UnknownFilter(table.Name, f.Column)
	}
	ref := colRef(alias, col.Name)
	switch f.Op {
	case "eq":
		return op.Eq(ref, f.Value).GetSQL(), nil
	case "ne":
		return op.Ne(ref, f.Value).GetSQL(), nil
	case "gt":
		return op.Gt(ref, f.Value).GetSQL(), nil
	case "gte":
		return op.Gte(ref, f.Value).GetSQL(), nil
	case "lt":
		return op.Lt(ref, f.Value).GetSQL(), nil
	case "lte":
		return op.Lte(ref, f.Value).GetSQL(), nil
	case "like":
		return op.Like(ref, f.Value), nil
	case "ilike":
		return op.ILike(ref, f.Value), nil
	case "notLike":
		return op.NotLike(ref, f.Value), nil
	case "notIlike":
		return op.NotILike(ref, f.Value), nil
	case "isNull":
		return op.IsNull(ref), nil
	case "isNotNull":
		return op.IsNotNull(ref), nil
	case "in":
		vals, ok := f.Value.([]any)
		if !ok {
			return nil, fmt.Errorf("relation: table %s column %s: in expected []any", table.Name, f.Column)
		}
		return op.InArray(ref, vals), nil
	case "notIn":
		vals, ok := f.Value.([]any)
		if !ok {
			return nil, fmt.Errorf("relation: table %s column %s: notIn expected []any", table.Name, f.Column)
		}
		return op.NotInArray(ref, vals), nil
	case "between":
		return op.Between(ref, f.Value, f.Value2), nil
	case "notBetween":
		return op.NotBetween(ref, f.Value, f.Value2), nil
	default:
		return nil, fmt.Errorf("relation: table %s column %s: unknown comparison %q; expected eq, ne, gt, gte, lt, lte, like, ilike, notLike, notIlike, isNull, isNotNull, in, notIn, between, or notBetween", table.Name, f.Column, f.Op)
	}
}

func (q *rqb) relFilter(table *relation.Table, alias string, f relation.Filter, depth int) (*sql.SQL, bool, error) {
	rel, ok := table.Relation(f.Relation)
	if !ok {
		return nil, false, relation.UnknownFilter(table.Name, f.Relation)
	}
	target, ok := q.schema.Get(rel.Target)
	if !ok {
		return nil, false, fmt.Errorf("relation: table %q is not in the schema", rel.Target)
	}
	tAlias := fmt.Sprintf("f%d", depth)
	throughAlias := ""
	var junction *relation.Table
	if rel.Through != nil {
		junction, ok = q.schema.Get(rel.Through.Table)
		if !ok {
			return nil, false, fmt.Errorf("relation: table %q is not in the schema", rel.Through.Table)
		}
		throughAlias = fmt.Sprintf("ft%d", depth)
	}
	cond, on, err := q.relationCond(table, alias, rel, tAlias, throughAlias)
	if err != nil {
		return nil, false, err
	}
	where := cond
	if f.Nested != nil {
		inner, _, err := q.compileFilter(target, tAlias, *f.Nested, depth+1)
		if err != nil {
			return nil, false, err
		}
		where = sql.And(cond, inner)
	}
	word := "exists "
	if !f.Exists {
		word = "not exists "
	}
	parts := []any{
		sql.Fragment(word + "(select * from "),
		sql.Identifier(target.SQLName), sql.Raw(" as "), sql.Identifier(tAlias),
	}
	if junction != nil {
		parts = append(parts,
			sql.Fragment(" inner join "),
			sql.Identifier(junction.SQLName), sql.Raw(" as "), sql.Identifier(throughAlias),
			sql.Fragment(" on "),
			on,
		)
	}
	parts = append(parts, sql.Fragment(" where "), where, sql.Fragment(")"))
	return sql.S(parts...), true, nil
}

func (q *rqb) relationCond(src *relation.Table, srcAlias string, rel relation.Relation, dstAlias, throughAlias string) (where, on *sql.SQL, err error) {
	if rel.Through != nil && throughAlias == "" {
		return nil, nil, fmt.Errorf("relation: relation %q through is missing an alias", rel.Name)
	}
	parts := make([]sql.Chunk, 0, len(rel.From)+1)
	if rel.Through == nil {
		for i := range rel.From {
			parts = append(parts, op.Eq(colRef(srcAlias, rel.From[i]), colRef(dstAlias, rel.To[i])))
		}
	} else {
		for i := range rel.From {
			parts = append(parts, op.Eq(colRef(srcAlias, rel.From[i]), colRef(throughAlias, rel.Through.From[i])))
		}
		onParts := make([]sql.Chunk, 0, len(rel.To))
		for i := range rel.To {
			onParts = append(onParts, op.Eq(colRef(throughAlias, rel.Through.To[i]), colRef(dstAlias, rel.To[i])))
		}
		on = sql.And(onParts...)
	}
	if rel.Where != nil {
		wtable := src
		walias := srcAlias
		if !rel.Reversed {
			var ok bool
			wtable, ok = q.schema.Get(rel.Target)
			if !ok {
				return nil, nil, fmt.Errorf("relation: table %q is not in the schema", rel.Target)
			}
			walias = dstAlias
		}
		w, _, err := q.compileFilter(wtable, walias, *rel.Where, 0)
		if err != nil {
			return nil, nil, err
		}
		if w != nil {
			parts = append(parts, w)
		}
	}
	return sql.And(parts...), on, nil
}
