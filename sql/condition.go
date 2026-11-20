package sql

// 本文件只包含“不碰列类型”的条件组合子，对应 drizzle 的
// sql/expressions/conditions.ts 里的 and / or / not 三个函数。
//
// 为什么它们在 M0 而不是 M2：这三个函数只操作 Wrapper，与列、编码器无关；
// 真正的列运算符（Eq / Gt / InArray ……）需要列类型与编码器，属于 M2。
// drizzle 的单元测试 sql-builder.test.ts 覆盖的正是这三个函数，所以它们
// 必须在 M0 就可用。

// And 用 and 连接条件，nil 条件（含 (*SQL)(nil)）自动忽略。
//
// 对应 drizzle 的 and：没有条件时返回 nil，单个条件时原样包一层，
// 多个条件时渲染为 `((c1) and (c2))`。
func And(conditions ...Chunk) *SQL {
	return joinConditions(conditions, Fragment(") and ("))
}

// Or 用 or 连接条件，语义与 And 相同，分隔符换成 `) or (`。
func Or(conditions ...Chunk) *SQL {
	return joinConditions(conditions, Fragment(") or ("))
}

// Not 取反一个条件。
//
// 与 drizzle 一致：条件本身是 SQL 时加一层括号（`not (...)`），
// 否则直接接在后面（`not "users"."id"`）—— 后者依赖该 chunk 自己的渲染分支。
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
