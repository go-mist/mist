package sqlite

import (
	"fmt"
	"strings"

	"github.com/xbmlz/mist/relation"
	"github.com/xbmlz/mist/sql"
)

// BuildRelationalQuery compiles one level of a relational query into an SQL fragment.
// mode is relation.ModeMany or relation.ModeFirst.
// SQLite has no LATERAL. Nested relations are correlated subqueries of json_group_array / json_object. See ADR-0014.
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
	sel, err := q.collect(tab, &cfg, mode, 0, nil, nil)
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

// sqliteShouldWrap decides whether the root query paginates before computing relations.
// A correlated subquery runs per result row. When there is a with and a limit, and an order was written, or an offset was written and where has no relation condition, wrap the current table first.
func sqliteShouldWrap(strategy string, withCount int, relFilters bool, limit, offset *int, hasOrder bool) bool {
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

type throughLink struct {
	sqlName string
	alias   string
	on      *sql.SQL
}

func (q *rqb) add(cs ...sql.Chunk) {
	q.chunks = append(q.chunks, cs...)
}

func (q *rqb) collect(table *relation.Table, cfg *relation.Config, mode string, depth int, relWhere *sql.SQL, link *throughLink) (relation.Selection, error) {
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
	if mode == relation.ModeFirst {
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
				return relation.Selection{}, fmt.Errorf("sqlite: JSON cast %q accepts only text", c.CastInJSON)
			}
			q.add(sql.Fragment("cast("), colRef(alias, c.Name), sql.Fragment(" as text)"))
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
		cond, on, err := q.relationCond(table, alias, rel, childAlias, throughAlias)
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
		ordered := childMode == relation.ModeMany && w.Query != nil && len(w.Query.Order.Items) > 0
		q.add(sql.Fragment("(select "))
		jsonAt := len(q.chunks)
		q.add(sql.Raw(""))
		q.add(sql.Fragment(" from ("))
		inner, err := q.collect(target, w.Query, childMode, depth+1, cond, childLink)
		if err != nil {
			return relation.Selection{}, err
		}
		expr, err := jsonExpr(inner, rel.Kind == relation.KindMany, ordered)
		if err != nil {
			return relation.Selection{}, err
		}
		q.chunks[jsonAt] = expr
		q.add(sql.Fragment(") as "), sql.Identifier("t"), sql.Fragment(") as "), sql.Identifier(rel.Name))
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
	if depth > 0 && mode == relation.ModeMany && orderSQL != nil {
		q.add(sql.Fragment(", row_number() over (order by "), orderSQL, sql.Fragment(") as "), sql.Identifier("ord"))
	}

	wrap := sqliteShouldWrap(q.strategy, len(cfg.With), hasRel, limit, cfg.Offset, orderSQL != nil)
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
	if !wrap {
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
	if limit != nil {
		q.add(sql.Fragment(" limit "), sql.NewParam(*limit, nil))
	}
	if offset != nil {
		q.add(sql.Fragment(" offset "), sql.NewParam(*offset, nil))
	}
}

func jsonExpr(sel relation.Selection, many, ordered bool) (sql.Chunk, error) {
	keys := make([]string, 0, len(sel.Columns)+len(sel.Extras)+len(sel.Rels))
	keys = append(keys, sel.Columns...)
	for _, ex := range sel.Extras {
		keys = append(keys, ex.Key)
	}
	rels := make(map[string]bool, len(sel.Rels))
	for _, rel := range sel.Rels {
		keys = append(keys, rel.Name)
		rels[rel.Name] = true
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("relation: JSON object has no fields")
	}
	var b strings.Builder
	if many {
		b.WriteString("coalesce(json_group_array(json_object(")
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
		if rels[key] {
			// json_object re-encodes text as a string. A relation column is already JSON text, so nest it as json("key").
			b.WriteString("json(")
			b.WriteString(d.EscapeName(key))
			b.WriteString(")")
			continue
		}
		b.WriteString(d.EscapeName(key))
	}
	if many {
		b.WriteString(")")
		if ordered {
			b.WriteString(" order by ")
			b.WriteString(d.EscapeName("ord"))
		}
		b.WriteString("), json_array())")
	} else {
		b.WriteString(")")
	}
	return sql.Raw(b.String()), nil
}

func colRef(alias, name string) *sql.SQL {
	return sql.S(sql.Identifier(alias), sql.Raw("."), sql.Identifier(name))
}
