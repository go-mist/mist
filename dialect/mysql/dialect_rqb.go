package mysql

import (
	"fmt"
	"strings"

	"github.com/go-mist/mist/relation"
	"github.com/go-mist/mist/sql"
)

// MySQL has no row_to_json / json_agg. Nested relations are correlated subqueries of
// json_arrayagg / json_object. MariaDB rejects LATERAL, and it also rejects an outer
// column inside a derived table, so the parent key is matched outside that table.
// Order and limit use row_number partitioned by the parent key. See ADR-0016.

// BuildRelationalQuery compiles one level of a relational query into an SQL fragment.
// mode is relation.ModeMany or relation.ModeFirst.
func BuildRelationalQuery(schema *relation.Schema, table string, cfg relation.Config, mode string) (relation.Built, error) {
	if schema == nil {
		return relation.Built{}, fmt.Errorf("relation: missing schema")
	}
	tab, ok := schema.Get(table)
	if !ok {
		return relation.Built{}, fmt.Errorf("relation: table %q is not in the schema", table)
	}
	if mode != relation.ModeMany && mode != relation.ModeFirst {
		return relation.Built{}, fmt.Errorf("relation: unknown mode %q", mode)
	}
	strategy, err := normalizeStrategy(cfg.Strategy)
	if err != nil {
		return relation.Built{}, err
	}
	q := &rqb{schema: schema, strategy: strategy}
	sel, err := q.collect(tab, &cfg, mode, 0, nil, nil, nil)
	if err != nil {
		return relation.Built{}, err
	}
	body, err := Dialect{}.expandSQL(&sql.SQL{QueryChunks: q.chunks})
	if err != nil {
		return relation.Built{}, err
	}
	return relation.Built{
		Query:     body,
		Table:     table,
		Selection: sel,
		WithCache: cfg.WithCache,
	}, nil
}

func normalizeStrategy(s string) (string, error) {
	switch s {
	case "", relation.StrategyOptimized:
		return relation.StrategyOptimized, nil
	case relation.StrategyBasic:
		return relation.StrategyBasic, nil
	default:
		return "", fmt.Errorf("relation: unknown strategy %q", s)
	}
}

// mysqlShouldWrap is the MySQL wrap condition.
// There is a with and a limit, and either order was written, or offset was written and where has no relational condition.
// Paging that exists only because of a relational condition is not wrapped. See ADR-0011.
func mysqlShouldWrap(strategy string, withCount int, relFilters bool, limit, offset *int, hasOrder bool) bool {
	if strategy == relation.StrategyBasic || withCount == 0 || limit == nil {
		return false
	}
	if hasOrder {
		return true
	}
	return offset != nil && !relFilters
}

type rqb struct {
	schema   *relation.Schema
	strategy string
	chunks   []sql.Chunk
}

// fkCol is one parent-key column. inner is selected inside the derived table;
// the comparison with outer is written after that table, where MariaDB can see the parent.
type fkCol struct {
	inner *sql.SQL
	outer *sql.SQL
	alias string
}

// outsideBind marks a relation subquery whose parent match is applied outside the derived table.
type outsideBind struct {
	fks []fkCol
}

// orderCol is the row_number alias. It is also written into the JSON object so the
// result can be sorted after MariaDB's json_arrayagg, which does not keep ORDER BY.
const orderCol = "mfkord"

func relationNeedsOrd(mode string, cfg *relation.Config) bool {
	if mode == relation.ModeFirst {
		return true
	}
	if cfg == nil {
		return false
	}
	return cfg.Limit != nil || cfg.Offset != nil || len(cfg.Order.Items) > 0
}

type throughLink struct {
	sqlName string
	alias   string
	on      *sql.SQL
}

func (q *rqb) add(cs ...sql.Chunk) {
	q.chunks = append(q.chunks, cs...)
}

func (q *rqb) collect(table *relation.Table, cfg *relation.Config, mode string, depth int, relWhere *sql.SQL, link *throughLink, outside *outsideBind) (relation.Selection, error) {
	if cfg == nil {
		cfg = &relation.Config{}
	}
	alias := fmt.Sprintf("d%d", depth)
	userWhere, hasRel, err := q.compileFilter(table, alias, cfg.Where, 0)
	if err != nil {
		return relation.Selection{}, err
	}
	where := sql.And(userWhere, relWhere)
	orderSQL, err := q.compileOrder(table, alias, cfg.Order)
	if err != nil {
		return relation.Selection{}, err
	}
	limit := cfg.Limit
	if mode == relation.ModeFirst && outside == nil {
		one := 1
		limit = &one
	}
	seen := make(map[string]bool, len(cfg.With))
	for _, w := range cfg.With {
		if w.Name == "" {
			return relation.Selection{}, fmt.Errorf("relation: table %q has a with entry with no name", table.Name)
		}
		if seen[w.Name] {
			return relation.Selection{}, fmt.Errorf("relation: table %q with %q is duplicated", table.Name, w.Name)
		}
		seen[w.Name] = true
		if _, ok := table.Relation(w.Name); !ok {
			return relation.Selection{}, relation.UnknownRelation(table.Name, w.Name)
		}
	}

	var sel relation.Selection
	q.add(sql.Fragment("select "))
	cols, err := table.Selected(cfg.Columns)
	if err != nil {
		return relation.Selection{}, err
	}
	wrote := false
	sep := func() {
		if wrote {
			q.add(sql.Fragment(", "))
		}
		wrote = true
	}
	for _, c := range cols {
		sep()
		if depth > 0 && c.CastInJSON != "" {
			if c.CastInJSON != "text" {
				return relation.Selection{}, fmt.Errorf("mysql: JSON cast %q accepts only text", c.CastInJSON)
			}
			q.add(sql.Fragment("cast("), colRef(alias, c.Name), sql.Fragment(" as char)"))
		} else {
			q.add(colRef(alias, c.Name))
		}
		q.add(sql.Fragment(" as "), sql.Identifier(c.Key))
		sel.Columns = append(sel.Columns, c.Key)
	}
	for _, ex := range cfg.Extras {
		if ex.Key == "" || ex.Expr == nil {
			return relation.Selection{}, fmt.Errorf("relation: table %q extras is missing a key or expression", table.Name)
		}
		sep()
		q.add(sql.Fragment("("), ex.Expr, sql.Fragment(") as "), sql.Identifier(ex.Key))
		sel.Extras = append(sel.Extras, relation.SelectedExtra{Key: ex.Key, Ptr: ex.Ptr, ReadJSON: ex.ReadJSON})
	}
	for _, w := range cfg.With {
		sep()
		rel, _ := table.Relation(w.Name)
		target, ok := q.schema.Get(rel.Target)
		if !ok {
			return relation.Selection{}, fmt.Errorf("relation: table %q is not in the schema", rel.Target)
		}
		childAlias := fmt.Sprintf("d%d", depth+1)
		throughAlias := ""
		var childLink *throughLink
		if rel.Through != nil {
			junction, ok := q.schema.Get(rel.Through.Table)
			if !ok {
				return relation.Selection{}, fmt.Errorf("relation: table %q is not in the schema", rel.Through.Table)
			}
			throughAlias = fmt.Sprintf("tr%d", depth)
			childLink = &throughLink{sqlName: junction.SQLName, alias: throughAlias}
		}
		on, innerWhere, outerWhere, fks, err := q.relationBind(table, alias, rel, childAlias, throughAlias)
		if err != nil {
			return relation.Selection{}, err
		}
		if childLink != nil {
			childLink.on = on
		}
		childMode := relation.ModeMany
		if rel.Kind == relation.KindOne {
			childMode = relation.ModeFirst
		}
		childCfg := w.Query
		if childCfg == nil {
			childCfg = &relation.Config{}
		}
		q.add(sql.Fragment("(select "))
		jsonAt := len(q.chunks)
		q.add(sql.Raw(""))
		q.add(sql.Fragment(" from ("))
		inner, err := q.collect(target, childCfg, childMode, depth+1, innerWhere, childLink, &outsideBind{fks: fks})
		if err != nil {
			return relation.Selection{}, err
		}
		expr, err := jsonExpr(inner, rel.Kind == relation.KindMany, rel.Kind == relation.KindMany && relationNeedsOrd(childMode, childCfg))
		if err != nil {
			return relation.Selection{}, err
		}
		q.chunks[jsonAt] = expr
		q.add(sql.Fragment(") as "), sql.Identifier("t"), sql.Fragment(" where "))
		for i, fk := range fks {
			if i > 0 {
				q.add(sql.Fragment(" and "))
			}
			q.add(fk.outer, sql.Fragment(" = "), sql.Identifier(fk.alias))
		}
		if outerWhere != nil {
			q.add(sql.Fragment(" and "), outerWhere)
		}
		if childMode == relation.ModeFirst {
			q.add(sql.Fragment(" and "), sql.Identifier(orderCol), sql.Fragment(" = 1"))
		} else if relationNeedsOrd(childMode, childCfg) {
			if childCfg.Offset != nil {
				q.add(sql.Fragment(" and "), sql.Identifier(orderCol), sql.Fragment(" > "), sql.NewParam(*childCfg.Offset, nil))
			}
			if childCfg.Limit != nil {
				hi := *childCfg.Limit
				if childCfg.Offset != nil {
					hi += *childCfg.Offset
				}
				q.add(sql.Fragment(" and "), sql.Identifier(orderCol), sql.Fragment(" <= "), sql.NewParam(hi, nil))
			}
			q.add(sql.Fragment(" order by "), sql.Identifier(orderCol))
		}
		q.add(sql.Fragment(") as "), sql.Identifier(rel.Name))
		sel.Rels = append(sel.Rels, relation.RelSelection{
			Name:   rel.Name,
			Target: rel.Target,
			Many:   rel.Kind == relation.KindMany,
			Inner:  inner,
		})
	}
	if !wrote {
		return relation.Selection{}, relation.NoFields(table.Name)
	}
	if outside != nil {
		for _, fk := range outside.fks {
			q.add(sql.Fragment(", "), fk.inner, sql.Fragment(" as "), sql.Identifier(fk.alias))
		}
		if relationNeedsOrd(mode, cfg) {
			q.add(sql.Fragment(", row_number() over (partition by "))
			for i, fk := range outside.fks {
				if i > 0 {
					q.add(sql.Fragment(", "))
				}
				q.add(fk.inner)
			}
			q.add(sql.Fragment(" order by "))
			if orderSQL != nil {
				q.add(orderSQL)
			} else {
				for i, fk := range outside.fks {
					if i > 0 {
						q.add(sql.Fragment(", "))
					}
					q.add(fk.inner)
				}
			}
			q.add(sql.Fragment(") as "), sql.Identifier(orderCol))
		}
	}

	wrap := outside == nil && mysqlShouldWrap(q.strategy, len(cfg.With), hasRel, limit, cfg.Offset, orderSQL != nil)
	q.add(sql.Fragment(" from "))
	if wrap {
		q.add(sql.Fragment("(select "), sql.Identifier(alias), sql.Fragment(".* from "))
	}
	q.add(sql.Identifier(table.SQLName), sql.Raw(" as "), sql.Identifier(alias))
	if link != nil {
		q.add(sql.Fragment(" inner join "), sql.Identifier(link.sqlName), sql.Raw(" as "), sql.Identifier(link.alias), sql.Fragment(" on "), link.on)
	}
	if wrap {
		if where != nil {
			q.add(sql.Fragment(" where "), where)
		}
		if orderSQL != nil {
			q.add(sql.Fragment(" order by "), orderSQL)
		}
		q.writeLimit(limit, cfg.Offset)
		q.add(sql.Fragment(") as "), sql.Identifier(alias))
	}

	if outside != nil {
		if where != nil {
			q.add(sql.Fragment(" where "), where)
		}
	} else if !wrap {
		if where != nil {
			q.add(sql.Fragment(" where "), where)
		}
		if orderSQL != nil {
			q.add(sql.Fragment(" order by "), orderSQL)
		}
		q.writeLimit(limit, cfg.Offset)
	} else if orderSQL != nil {
		q.add(sql.Fragment(" order by "), orderSQL)
	}
	if comment := relation.FormatComment(cfg.Comment, cfg.CommentTags); comment != "" {
		q.add(sql.Fragment(" " + comment))
	}
	return sel, nil
}

func (q *rqb) writeLimit(limit, offset *int) {
	if limit == nil && offset != nil {
		q.add(sql.Raw(" limit 18446744073709551615"))
	}
	if limit != nil {
		q.add(sql.Fragment(" limit "), sql.NewParam(*limit, nil))
	}
	if offset != nil {
		q.add(sql.Fragment(" offset "), sql.NewParam(*offset, nil))
	}
}

func jsonExpr(sel relation.Selection, many, withOrd bool) (sql.Chunk, error) {
	keys := make([]string, 0, len(sel.Columns)+len(sel.Extras)+len(sel.Rels))
	keys = append(keys, sel.Columns...)
	for _, ex := range sel.Extras {
		keys = append(keys, ex.Key)
	}
	for _, rel := range sel.Rels {
		keys = append(keys, rel.Name)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("relation: JSON object has no fields")
	}
	var b strings.Builder
	if many {
		b.WriteString("coalesce(json_arrayagg(json_object(")
	} else {
		b.WriteString("json_object(")
	}
	d := Dialect{}
	for i, key := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(d.EscapeString(key))
		b.WriteString(", ")
		// A relation column is the JSON value of a scalar subquery. Both MySQL and MariaDB embed it.
		b.WriteString(d.EscapeName(key))
	}
	if withOrd {
		b.WriteString(", ")
		b.WriteString(d.EscapeString(orderCol))
		b.WriteString(", ")
		b.WriteString(d.EscapeName(orderCol))
	}
	if many {
		b.WriteString(")), json_array())")
	} else {
		b.WriteString(")")
	}
	return sql.Raw(b.String()), nil
}

func colRef(alias, name string) *sql.SQL {
	return sql.S(sql.Identifier(alias), sql.Raw("."), sql.Identifier(name))
}
