package mssql

import (
	"fmt"

	"github.com/go-mist/mist/relation"
	"github.com/go-mist/mist/sql"
)

// SQL Server has APPLY and FOR JSON, not LATERAL or json_agg.
// A many relation is OUTER APPLY of a scalar FOR JSON PATH, coalesced to [].
// A to-one relation uses WITHOUT_ARRAY_WRAPPER. Nested JSON is wrapped in JSON_QUERY.
const (
	rqbManyOpen = " outer apply (select coalesce(("
	rqbOneOpen  = " outer apply (select ("
	rqbManyEnd  = " for json path), '[]') as [r]) as "
	rqbOneEnd   = " for json path, without_array_wrapper) as [r]) as "
	rqbRelField = ".[r] as "
)

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

// mssqlShouldWrap is the MySQL wrap condition.
// There is a with and a limit, and either order was written, or offset was written and where has no relational condition.
// Paging that exists only because of a relational condition is not wrapped. See ADR-0011.
func mssqlShouldWrap(strategy string, withCount int, relFilters bool, limit, offset *int, hasOrder bool) bool {
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
	// FETCH requires ORDER BY. A limit that the caller did not order is ordered by the first column.
	if orderSQL == nil && (limit != nil || cfg.Offset != nil) {
		orderSQL = sql.S(sql.Raw("1"))
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
				return relation.Selection{}, fmt.Errorf("mssql: JSON cast %q accepts only text", c.CastInJSON)
			}
			q.add(sql.Fragment("cast("), colRef(alias, c.Name), sql.Fragment(" as nvarchar(max))"))
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
		if depth > 0 {
			q.add(sql.Fragment("json_query("), sql.Identifier(w.Name), sql.Fragment(".[r]) as "), sql.Identifier(w.Name))
			continue
		}
		q.add(sql.Identifier(w.Name), sql.Fragment(rqbRelField), sql.Identifier(w.Name))
	}
	if !wrote {
		return relation.Selection{}, relation.NoFields(table.Name)
	}
	wrap := mssqlShouldWrap(q.strategy, len(cfg.With), hasRel, limit, cfg.Offset, orderSQL != nil)
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
		if err := q.writeLimit(limit, cfg.Offset, orderSQL != nil); err != nil {
			return relation.Selection{}, err
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
		childMode := relation.ModeMany
		if rel.Kind == relation.KindOne {
			childMode = relation.ModeFirst
		}
		if rel.Kind == relation.KindMany {
			q.add(sql.Fragment(rqbManyOpen))
		} else {
			q.add(sql.Fragment(rqbOneOpen))
		}
		inner, err := q.collect(target, w.Query, childMode, depth+1, cond, childLink)
		if err != nil {
			return relation.Selection{}, err
		}
		if rel.Kind == relation.KindMany {
			q.add(sql.Fragment(rqbManyEnd), sql.Identifier(rel.Name))
		} else {
			q.add(sql.Fragment(rqbOneEnd), sql.Identifier(rel.Name))
		}
		sel.Rels = append(sel.Rels, relation.RelSelection{
			Name:   rel.Name,
			Target: rel.Target,
			Many:   rel.Kind == relation.KindMany,
			Inner:  inner,
		})
	}

	if !wrap {
		if where != nil {
			q.add(sql.Fragment(" where "), where)
		}
		if orderSQL != nil {
			q.add(sql.Fragment(" order by "), orderSQL)
		}
		if err := q.writeLimit(limit, cfg.Offset, orderSQL != nil); err != nil {
			return relation.Selection{}, err
		}
	} else if orderSQL != nil {
		q.add(sql.Fragment(" order by "), orderSQL)
	}
	if comment := relation.FormatComment(cfg.Comment, cfg.CommentTags); comment != "" {
		q.add(sql.Fragment(" " + comment))
	}
	return sel, nil
}

func (q *rqb) writeLimit(limit, offset *int, hasOrder bool) error {
	if (limit != nil || offset != nil) && !hasOrder {
		return fmt.Errorf("mssql: offset and fetch require order by")
	}
	if offset != nil {
		q.add(sql.Fragment(" offset "), sql.NewParam(*offset, nil), sql.Raw(" rows"))
	} else if limit != nil {
		q.add(sql.Raw(" offset 0 rows"))
	}
	if limit != nil {
		q.add(sql.Fragment(" fetch next "), sql.NewParam(*limit, nil), sql.Raw(" rows only"))
	}
	return nil
}

func colRef(alias, name string) *sql.SQL {
	return sql.S(sql.Identifier(alias), sql.Raw("."), sql.Identifier(name))
}
