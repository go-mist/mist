package pg

import (
	"fmt"

	"github.com/xbmlz/mist/relation"
	"github.com/xbmlz/mist/sql"
)

type orderSource struct {
	from       []sql.Chunk
	filters    []sql.Chunk
	aggregated bool
	depth      int
}

func (q *rqb) compileOrder(table *relation.Table, alias string, order relation.Order) (*sql.SQL, error) {
	if len(order.Items) == 0 {
		return nil, nil
	}
	parts := make([]sql.Chunk, 0, len(order.Items))
	for _, item := range order.Items {
		ch, err := q.orderItem(table, alias, item, nil, itemPath(item))
		if err != nil {
			return nil, err
		}
		parts = append(parts, ch)
	}
	return sql.Join(parts, sql.Fragment(", ")), nil
}

func itemPath(item relation.OrderItem) string {
	if item.Column != "" {
		return item.Column
	}
	return item.Relation
}

func (q *rqb) orderItem(table *relation.Table, alias string, item relation.OrderItem, src *orderSource, path string) (sql.Chunk, error) {
	if item.Column != "" && item.Relation != "" {
		return nil, fmt.Errorf("relation: order %q cannot be both a column and a relation", path)
	}
	if item.Column == "" && item.Relation == "" {
		return nil, fmt.Errorf("relation: order %q is empty", path)
	}
	if item.Column != "" {
		return q.orderColumn(table, alias, item, src, path)
	}
	return q.orderRel(table, alias, item, src, path)
}

func (q *rqb) orderColumn(table *relation.Table, alias string, item relation.OrderItem, src *orderSource, path string) (sql.Chunk, error) {
	if item.Dir != "asc" && item.Dir != "desc" {
		return nil, fmt.Errorf("relation: order %q direction must be asc or desc", path)
	}
	col, ok := table.Column(item.Column)
	if !ok {
		return nil, fmt.Errorf("relation: unknown order field %q", path)
	}
	aggregated := src != nil && src.aggregated
	if (item.Aggregate != "") != aggregated {
		if aggregated {
			return nil, fmt.Errorf("relation: order %q requires min, max, count, avg, or first", path)
		}
		return nil, fmt.Errorf("relation: order %q cannot take an aggregate", path)
	}
	if item.Aggregate != "" && !knownAgg(item.Aggregate) {
		return nil, fmt.Errorf("relation: order %q aggregate %q is unknown", path, item.Aggregate)
	}
	ref := colRef(alias, col.Name)
	if src == nil {
		return sql.S(ref, sql.Fragment(" "+item.Dir)), nil
	}
	single := item.Aggregate == "" || item.Aggregate == "first"
	parts := []any{sql.Fragment("(select ")}
	if single {
		parts = append(parts, ref)
	} else {
		parts = append(parts, sql.Fragment(item.Aggregate+"("), ref, sql.Fragment(")"))
	}
	parts = append(parts, sql.Fragment(" from "))
	for _, ch := range src.from {
		parts = append(parts, ch)
	}
	if len(src.filters) > 0 {
		parts = append(parts, sql.Fragment(" where "), sql.And(src.filters...))
	}
	if single {
		parts = append(parts, sql.Fragment(" limit 1"))
	}
	parts = append(parts, sql.Fragment(")"), sql.Fragment(" "+item.Dir))
	return sql.S(parts...), nil
}

func (q *rqb) orderRel(table *relation.Table, alias string, item relation.OrderItem, src *orderSource, path string) (sql.Chunk, error) {
	rel, ok := table.Relation(item.Relation)
	if !ok {
		return nil, fmt.Errorf("relation: unknown order field %q", path)
	}
	target, ok := q.schema.Get(rel.Target)
	if !ok {
		return nil, fmt.Errorf("relation: table %q is not in the schema", rel.Target)
	}
	depth := 0
	if src != nil {
		depth = src.depth
	}
	tAlias := fmt.Sprintf("o%d", depth)
	throughAlias := ""
	var junction *relation.Table
	if rel.Through != nil {
		junction, ok = q.schema.Get(rel.Through.Table)
		if !ok {
			return nil, fmt.Errorf("relation: table %q is not in the schema", rel.Through.Table)
		}
		throughAlias = fmt.Sprintf("ot%d", depth)
	}
	var from []sql.Chunk
	if src != nil {
		from = append(from, src.from...)
		from = append(from, sql.Fragment(" cross join "))
	}
	from = append(from, sql.Identifier(target.SQLName), sql.Raw(" as "), sql.Identifier(tAlias))
	cond, on, err := q.relationCond(table, alias, rel, tAlias, throughAlias)
	if err != nil {
		return nil, err
	}
	if junction != nil {
		from = append(from,
			sql.Fragment(" inner join "),
			sql.Identifier(junction.SQLName), sql.Raw(" as "), sql.Identifier(throughAlias),
			sql.Fragment(" on "),
			on,
		)
	}
	filters := make([]sql.Chunk, 0, 1)
	if src != nil {
		filters = append(filters, src.filters...)
	}
	if cond != nil {
		filters = append(filters, cond)
	}
	if item.Nested == nil {
		if item.Aggregate != "count" || (item.Dir != "asc" && item.Dir != "desc") || rel.Kind != relation.KindMany {
			return nil, fmt.Errorf("relation: order %q needs an object; a many relation may use asc(count) or desc(count)", path)
		}
		parts := []any{sql.Fragment("(select count(*) from ")}
		for _, ch := range from {
			parts = append(parts, ch)
		}
		if len(filters) > 0 {
			parts = append(parts, sql.Fragment(" where "), sql.And(filters...))
		}
		parts = append(parts, sql.Fragment(")"), sql.Fragment(" "+item.Dir))
		return sql.S(parts...), nil
	}
	if item.Dir != "" || item.Aggregate != "" {
		return nil, fmt.Errorf("relation: order %q object cannot also set a direction", path)
	}
	next := &orderSource{
		from:       from,
		filters:    filters,
		aggregated: (src != nil && src.aggregated) || rel.Kind == relation.KindMany,
		depth:      depth + 1,
	}
	parts := make([]sql.Chunk, 0, len(item.Nested.Items))
	for _, child := range item.Nested.Items {
		ch, err := q.orderItem(target, tAlias, child, next, path+"."+itemPath(child))
		if err != nil {
			return nil, err
		}
		parts = append(parts, ch)
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("relation: order %q is empty", path)
	}
	return sql.Join(parts, sql.Fragment(", ")), nil
}

func knownAgg(name string) bool {
	switch name {
	case "min", "max", "count", "avg", "first":
		return true
	default:
		return false
	}
}
