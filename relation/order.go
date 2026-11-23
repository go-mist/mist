package relation

// OrderItem is one entry in an ordering. A column order fills Column.
// `desc(count)` on a relation fills Relation and Aggregate.
// An order inside a relation fills Relation and Nested.
type OrderItem struct {
	Column    string
	Dir       string
	Aggregate string
	Relation  string
	Nested    *Order
}

// Order is a sequence of order entries. That sequence is the SQL order.
type Order struct {
	Items []OrderItem
}

// Asc orders a column ascending.
func Asc(col string) Order {
	return Order{Items: []OrderItem{{Column: col, Dir: "asc"}}}
}

// Desc orders a column descending.
func Desc(col string) Order {
	return Order{Items: []OrderItem{{Column: col, Dir: "desc"}}}
}

// Aggregate is an aggregate order of a column across a relation, for example `desc(max)`.
// fn must be one of min, max, count, avg, or first.
func Aggregate(col, dir, fn string) Order {
	return Order{Items: []OrderItem{{Column: col, Dir: dir, Aggregate: fn}}}
}

// CountOrder orders by the row count of a many relation, matching `asc(count)` / `desc(count)`.
func CountOrder(rel, dir string) Order {
	return Order{Items: []OrderItem{{Relation: rel, Dir: dir, Aggregate: "count"}}}
}

// ByRel orders by a column inside one relation, or by a deeper relation.
func ByRel(rel string, nested Order) Order {
	n := nested
	return Order{Items: []OrderItem{{Relation: rel, Nested: &n}}}
}

// Orders concatenates orders in argument order.
func Orders(parts ...Order) Order {
	var items []OrderItem
	for _, p := range parts {
		items = append(items, p.Items...)
	}
	return Order{Items: items}
}
