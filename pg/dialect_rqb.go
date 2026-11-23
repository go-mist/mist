package pg

import (
	"fmt"

	"github.com/xbmlz/mist/relation"
	"github.com/xbmlz/mist/sql"
)

// These fragments match the rc.5 pg-core dialect rqbChunks verbatim.
const (
	rqbJoinOne  = ` left join lateral(select row_to_json("t".*) "r" from (`
	rqbJoinMany = ` left join lateral(select coalesce(json_agg(row_to_json("t".*)), '[]') as "r" from (`
	rqbJoinEnd  = `) as "t") as `
	rqbOnTrue   = ` on true`
	rqbRelField = `."r" as `
)

// BuildRelationalQuery compiles one layer of a relational query into an SQL fragment.
// mode is relation.ModeMany or relation.ModeFirst.
// Rendering still goes through sql.Render; this does not touch the connection.
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
	return relation.Built{
		Query:     &sql.SQL{QueryChunks: q.chunks},
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

type rqb struct {
	schema   *relation.Schema
	strategy string
	chunks   []sql.Chunk
}

// throughLink is the through table on this layer's FROM. The parent layer picks the alias by role: tr, ft, or ot.
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
		q.add(colRef(alias, c.Name))
		if depth > 0 && c.CastInJSON != "" {
			if err := jsonCastName(c.CastInJSON); err != nil {
				return relation.Selection{}, err
			}
			q.add(sql.Fragment("::" + c.CastInJSON))
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
		q.add(sql.Identifier(w.Name), sql.Fragment(rqbRelField), sql.Identifier(w.Name))
	}
	if !wrote {
		return relation.Selection{}, relation.NoFields(table.Name)
	}

	paginated := cfg.Offset != nil || (limit != nil && orderSQL != nil)
	wrap := relation.ShouldWrap(q.strategy, len(cfg.With), hasRel, limit != nil, cfg.Offset != nil, orderSQL != nil)
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
		if paginated {
			q.writePage(orderSQL, limit, cfg.Offset)
		}
		q.add(sql.Fragment(") as "), sql.Identifier(alias))
	}

	for _, w := range cfg.With {
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
		if rel.Kind == relation.KindMany {
			q.add(sql.Fragment(rqbJoinMany))
		} else {
			q.add(sql.Fragment(rqbJoinOne))
		}
		childMode := relation.ModeMany
		if rel.Kind == relation.KindOne {
			childMode = relation.ModeFirst
		}
		inner, err := q.collect(target, w.Query, childMode, depth+1, cond, childLink)
		if err != nil {
			return relation.Selection{}, err
		}
		q.add(sql.Fragment(rqbJoinEnd), sql.Identifier(rel.Name), sql.Fragment(rqbOnTrue))
		sel.Rels = append(sel.Rels, relation.RelSelection{
			Name:   rel.Name,
			Target: rel.Target,
			Many:   rel.Kind == relation.KindMany,
			Inner:  inner,
		})
	}

	if where != nil && !wrap {
		q.add(sql.Fragment(" where "), where)
	}
	if orderSQL != nil {
		q.add(sql.Fragment(" order by "), orderSQL)
	}
	if !wrap || !paginated {
		q.writeLimit(limit, cfg.Offset)
	}
	if comment := relation.FormatComment(cfg.Comment, cfg.CommentTags); comment != "" {
		q.add(sql.Fragment(" " + comment))
	}
	return sel, nil
}

func (q *rqb) writePage(order *sql.SQL, limit, offset *int) {
	if order != nil {
		q.add(sql.Fragment(" order by "), order)
	}
	q.writeLimit(limit, offset)
}

func (q *rqb) writeLimit(limit, offset *int) {
	if limit != nil {
		q.add(sql.Fragment(" limit "), sql.NewParam(*limit, nil))
	}
	if offset != nil {
		q.add(sql.Fragment(" offset "), sql.NewParam(*offset, nil))
	}
}

func jsonCastName(name string) error {
	if name == "" {
		return fmt.Errorf("relation: JSON cast is empty")
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		ok := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9')
		if !ok {
			return fmt.Errorf("relation: JSON cast %q is not a safe type name", name)
		}
	}
	return nil
}

func colRef(alias, name string) *sql.SQL {
	return sql.S(sql.Identifier(alias), sql.Raw("."), sql.Identifier(name))
}
