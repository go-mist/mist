package relation

// See ADR-0005 for how the two strategies differ in SQL.
const (
	// StrategyOptimized is the default. When this level has relations and also has pagination or a relation filter, the table is wrapped in a subquery.
	StrategyOptimized = "optimized"
	// StrategyBasic does not wrap. where, order by, limit, and offset stay on the outer query of this level.
	StrategyBasic = "basic"
)

// ModeMany is findMany. ModeFirst is findFirst, and the inner limit is fixed at 1.
const (
	ModeMany  = "many"
	ModeFirst = "first"
)

// ShouldWrap reports whether this level should be wrapped as `(select alias.* from ...) as alias`.
//
// basic never wraps. A level without with does not wrap either. It wraps when any of
// offset, limit together with order, or a relation filter in where holds. The join's own equality does not count.
func ShouldWrap(strategy string, withCount int, relFilters, hasLimit, hasOffset, hasOrder bool) bool {
	if strategy == StrategyBasic || withCount == 0 {
		return false
	}
	if hasOffset || (hasLimit && hasOrder) || relFilters {
		return true
	}
	return false
}
