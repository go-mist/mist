package sql

// This file contains only condition combinators that do not touch column types, matching drizzle's
// and / or / not functions in sql/expressions/conditions.ts.
//
// Why they live in M0 rather than M2: these three functions operate only on Wrapper and do not depend on columns or encoders;
// real column operators (Eq / Gt / InArray ...) need column types and encoders and belong to M2.
// drizzle's unit test sql-builder.test.ts covers exactly these three functions, so they
// must be usable in M0.

// And joins conditions with and. Nil conditions (including (*SQL)(nil)) are ignored.
//
// Matches drizzle's and: return nil when there are no conditions, wrap a single condition as-is,
// and render multiple conditions as `((c1) and (c2))`.
func And(conditions ...Chunk) *SQL {
	return joinConditions(conditions, Fragment(") and ("))
}

// Or joins conditions with or. The semantics match And, with the separator replaced by `) or (`.
func Or(conditions ...Chunk) *SQL {
	return joinConditions(conditions, Fragment(") or ("))
}

// Not negates a condition.
//
// Matches drizzle: when the condition itself is SQL, wrap it in parentheses (`not (...)`);
// otherwise append it directly (`not "users"."id"`) — the latter depends on that chunk's own render branch.
func Not(condition Chunk) *SQL {
	if isNilChunk(condition) {
		return nil
	}
	if _, ok := condition.(*SQL); ok {
		return S(Fragment("not ("), condition, Fragment(")"))
	}
	return S(Fragment("not "), condition)
}

func joinConditions(conditions []Chunk, separator Fragment) *SQL {
	kept := make([]Chunk, 0, len(conditions))
	for _, c := range conditions {
		if isNilChunk(c) {
			continue
		}
		kept = append(kept, c)
	}

	switch len(kept) {
	case 0:
		return nil
	case 1:
		return &SQL{QueryChunks: kept}
	}

	chunks := make([]Chunk, 0, len(kept)*2+2)
	chunks = append(chunks, Fragment("(("))
	for i, c := range kept {
		if i > 0 {
			chunks = append(chunks, separator)
		}
		chunks = append(chunks, c)
	}
	chunks = append(chunks, Fragment("))"))
	return &SQL{QueryChunks: chunks}
}
