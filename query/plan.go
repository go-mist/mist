package query

import (
	"context"
	"strings"
	"sync/atomic"
	"unsafe"

	"github.com/go-mist/mist/sql"
	"github.com/go-mist/mist/sql/dialect"
)

// planKey is one simple statement shape. Pointer identity is stable for a package-level table or column.
// A value copied into an interface has no StablePtr, so it is not cached.
type planKey struct {
	typ   uintptr
	p0    uintptr
	p1    uintptr
	p2    uintptr
	p3    uintptr
	p4    uintptr
	p5    uintptr
	kind  byte
	n     byte
	op    byte
	flags byte
}

// planEntry is immutable after it is published.
type planEntry struct {
	k   planKey
	sql string
}

const planSlots = 16

var planCache [planSlots]atomic.Pointer[planEntry]

func loadPlan(k planKey) (string, bool) {
	e := planCache[planSlot(k)].Load()
	if e != nil && e.k == k {
		return e.sql, true
	}
	return "", false
}

func storePlan(k planKey, s string) string {
	planCache[planSlot(k)].Store(&planEntry{k: k, sql: s})
	return s
}

func planSlot(k planKey) uintptr {
	h := k.p0 ^ (k.p1 << 1) ^ (k.p2 << 2) ^ (uintptr(k.kind) << 8) ^ uintptr(k.op)
	return h % planSlots
}

func typePtr(v any) uintptr {
	type iface struct {
		typ  unsafe.Pointer
		data unsafe.Pointer
	}
	return uintptr((*iface)(unsafe.Pointer(&v)).typ)
}

func stablePtr(v any) (uintptr, bool) {
	s, ok := v.(interface{ StablePtr() uintptr })
	if !ok || s == nil {
		return 0, false
	}
	p := s.StablePtr()
	if p == 0 {
		return 0, false
	}
	return p, true
}

func identOf(v any) (table, name string, ok bool) {
	n, ok := v.(interface{ Ident() (string, string) })
	if !ok || n == nil {
		return "", "", false
	}
	table, name = n.Ident()
	return table, name, name != ""
}

func chunkEncoder(ch sql.Chunk) sql.Encoder {
	e, ok := ch.(interface{ DriverEncoder() sql.Encoder })
	if !ok || e == nil {
		return nil
	}
	return e.DriverEncoder()
}

func encoded(ch sql.Chunk) bool {
	enc := chunkEncoder(ch)
	return enc != nil && !enc.IsNoop()
}

func plainValue(v any) bool {
	if v == nil {
		return true
	}
	ch, ok := v.(sql.Chunk)
	return !ok || ch == nil
}

type comparer interface {
	Compare() (sql.Chunk, string, any)
}

func writeQual(b *strings.Builder, d dialect.Dialect, table, name string) {
	if table != "" {
		b.WriteString(d.EscapeName(table))
		b.WriteByte('.')
	}
	b.WriteString(d.EscapeName(name))
}

func (ins Insert[T]) tryFastExec(ctx context.Context, db Executor) (int64, bool, error) {
	if ins.cols != nil || !ins.one || ins.tail != nil || ins.nCols == 0 || ins.nCols != ins.nVals || ins.nCols > len(ins.colBuf) {
		return 0, false, nil
	}
	if ins.nothing || ins.duplicate || ins.ignore || ins.sel != nil || ins.sets != nil ||
		len(ins.withs) > 0 || len(ins.conflict) > 0 || len(ins.returning) > 0 ||
		ins.conflictWhere != nil || ins.setWhere != nil || ins.table == nil {
		return 0, false, nil
	}
	tab, ok := stablePtr(ins.table)
	if !ok {
		return 0, false, nil
	}
	var k planKey
	k.typ = typePtr(db)
	k.kind = 'i'
	k.n = byte(ins.nCols)
	k.p0 = tab
	for i := 0; i < ins.nCols; i++ {
		col := ins.colBuf[i]
		if col == nil || !plainValue(ins.oneRow[i]) {
			return 0, false, nil
		}
		if enc := col.DriverEncoder(); enc != nil && !enc.IsNoop() {
			return 0, false, nil
		}
		p, ok := stablePtr(col)
		if !ok {
			return 0, false, nil
		}
		switch i {
		case 0:
			k.p1 = p
		case 1:
			k.p2 = p
		case 2:
			k.p3 = p
		default:
			k.p4 = p
		}
	}
	text, ok := loadPlan(k)
	if !ok {
		built, ok := ins.buildInsert(db)
		if !ok {
			return 0, false, nil
		}
		text = storePlan(k, built)
	}
	var (
		n   int64
		err error
	)
	switch ins.nVals {
	case 1:
		n, err = db.Exec(ctx, text, ins.oneRow[0])
	case 2:
		n, err = db.Exec(ctx, text, ins.oneRow[0], ins.oneRow[1])
	case 3:
		n, err = db.Exec(ctx, text, ins.oneRow[0], ins.oneRow[1], ins.oneRow[2])
	default:
		n, err = db.Exec(ctx, text, ins.oneRow[0], ins.oneRow[1], ins.oneRow[2], ins.oneRow[3])
	}
	return n, true, err
}

func (ins Insert[T]) buildInsert(d dialect.Dialect) (string, bool) {
	table, name, ok := identOf(ins.table)
	if !ok {
		return "", false
	}
	var b strings.Builder
	b.Grow(64)
	b.WriteString("insert into ")
	writeQual(&b, d, table, name)
	b.WriteString(" (")
	for i := 0; i < ins.nCols; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		col := ins.colBuf[i]
		if col == nil || col.ColName() == "" {
			return "", false
		}
		b.WriteString(d.EscapeName(col.ColName()))
	}
	b.WriteString(") values (")
	for i := 0; i < ins.nCols; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(d.EscapeParam(i, nil))
	}
	b.WriteByte(')')
	return b.String(), true
}

func (u Update[T]) tryFastExec(ctx context.Context, db Executor) (int64, bool, error) {
	if u.sets != nil || u.nSets == 0 || u.nSets > len(u.setCols) || u.from != nil || u.joins != nil ||
		len(u.order) > 0 || u.limitSet || len(u.returning) > 0 || len(u.withs) > 0 || u.table == nil {
		return 0, false, nil
	}
	tab, ok := stablePtr(u.table)
	if !ok {
		return 0, false, nil
	}
	var k planKey
	k.typ = typePtr(db)
	k.kind = 'u'
	k.n = byte(u.nSets)
	k.p0 = tab
	for i := 0; i < u.nSets; i++ {
		col := u.setCols[i]
		if col == nil || !plainValue(u.setVals[i]) {
			return 0, false, nil
		}
		if enc := col.DriverEncoder(); enc != nil && !enc.IsNoop() {
			return 0, false, nil
		}
		p, ok := stablePtr(col)
		if !ok {
			return 0, false, nil
		}
		switch i {
		case 0:
			k.p1 = p
		case 1:
			k.p2 = p
		case 2:
			k.p3 = p
		default:
			k.p4 = p
		}
	}
	var whereRight any
	if u.where != nil {
		left, op, right, ok := plainCompare(u.where)
		if !ok {
			return 0, false, nil
		}
		p, ok := stablePtr(left)
		if !ok {
			return 0, false, nil
		}
		code := opCode(op)
		if code == 0 {
			return 0, false, nil
		}
		k.p5 = p
		k.op = code
		whereRight = right
	}
	text, ok := loadPlan(k)
	if !ok {
		built, ok := u.buildUpdate(db)
		if !ok {
			return 0, false, nil
		}
		text = storePlan(k, built)
	}
	var (
		n   int64
		err error
	)
	switch u.nSets {
	case 1:
		if u.where == nil {
			n, err = db.Exec(ctx, text, u.setVals[0])
		} else {
			n, err = db.Exec(ctx, text, u.setVals[0], whereRight)
		}
	case 2:
		if u.where == nil {
			n, err = db.Exec(ctx, text, u.setVals[0], u.setVals[1])
		} else {
			n, err = db.Exec(ctx, text, u.setVals[0], u.setVals[1], whereRight)
		}
	case 3:
		if u.where == nil {
			n, err = db.Exec(ctx, text, u.setVals[0], u.setVals[1], u.setVals[2])
		} else {
			n, err = db.Exec(ctx, text, u.setVals[0], u.setVals[1], u.setVals[2], whereRight)
		}
	default:
		if u.where == nil {
			n, err = db.Exec(ctx, text, u.setVals[0], u.setVals[1], u.setVals[2], u.setVals[3])
		} else {
			n, err = db.Exec(ctx, text, u.setVals[0], u.setVals[1], u.setVals[2], u.setVals[3], whereRight)
		}
	}
	return n, true, err
}

func (u Update[T]) buildUpdate(d dialect.Dialect) (string, bool) {
	table, name, ok := identOf(u.table)
	if !ok {
		return "", false
	}
	var b strings.Builder
	b.Grow(64)
	b.WriteString("update ")
	writeQual(&b, d, table, name)
	b.WriteString(" set ")
	for i := 0; i < u.nSets; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		col := u.setCols[i]
		if col == nil || col.ColName() == "" {
			return "", false
		}
		b.WriteString(d.EscapeName(col.ColName()))
		b.WriteString(" = ")
		b.WriteString(d.EscapeParam(i, nil))
	}
	if u.where != nil {
		left, op, _, ok := plainCompare(u.where)
		if !ok {
			return "", false
		}
		lt, ln, ok := identOf(left)
		if !ok {
			return "", false
		}
		b.WriteString(" where ")
		writeQual(&b, d, lt, ln)
		b.WriteString(op)
		b.WriteString(d.EscapeParam(u.nSets, nil))
	}
	return b.String(), true
}

func (s Select[T]) tryFastQuery(ctx context.Context, db Executor) (Rows, bool, error) {
	if s.tail != nil || s.distinct || len(s.distinctOn) > 0 || len(s.order) > 0 || s.picked != nil ||
		s.nPick == 0 || s.nPick > len(s.pickBuf) || s.from == nil || (s.offsetSet && !s.limitSet) {
		return nil, false, nil
	}
	from, ok := stablePtr(s.from)
	if !ok {
		return nil, false, nil
	}
	var k planKey
	k.typ = typePtr(db)
	k.kind = 's'
	k.n = byte(s.nPick)
	k.p0 = from
	for i := 0; i < s.nPick; i++ {
		col := s.pickBuf[i]
		if col == nil {
			return nil, false, nil
		}
		if enc := col.DriverEncoder(); enc != nil && !enc.IsNoop() {
			return nil, false, nil
		}
		p, ok := stablePtr(col)
		if !ok {
			return nil, false, nil
		}
		switch i {
		case 0:
			k.p1 = p
		case 1:
			k.p2 = p
		case 2:
			k.p3 = p
		default:
			k.p4 = p
		}
	}
	var whereRight any
	if s.where != nil {
		left, op, right, ok := plainCompare(s.where)
		if !ok {
			return nil, false, nil
		}
		p, ok := stablePtr(left)
		if !ok {
			return nil, false, nil
		}
		code := opCode(op)
		if code == 0 {
			return nil, false, nil
		}
		k.p5 = p
		k.op = code
		whereRight = right
	}
	if s.limitSet {
		k.flags |= 1
	}
	if s.offsetSet {
		k.flags |= 2
	}
	text, ok := loadPlan(k)
	if !ok {
		built, ok := s.buildSelect(db)
		if !ok {
			return nil, false, nil
		}
		text = storePlan(k, built)
	}
	var args [4]any
	n := 0
	if s.where != nil {
		args[n] = whereRight
		n++
	}
	if s.limitSet {
		args[n] = s.limit
		n++
	}
	if s.offsetSet {
		args[n] = s.offset
		n++
	}
	var (
		rows Rows
		err  error
	)
	switch n {
	case 0:
		rows, err = db.Query(ctx, text)
	case 1:
		rows, err = db.Query(ctx, text, args[0])
	case 2:
		rows, err = db.Query(ctx, text, args[0], args[1])
	case 3:
		rows, err = db.Query(ctx, text, args[0], args[1], args[2])
	default:
		rows, err = db.Query(ctx, text, args[0], args[1], args[2], args[3])
	}
	return rows, true, err
}

func (s Select[T]) buildSelect(d dialect.Dialect) (string, bool) {
	ft, fn, ok := identOf(s.from)
	if !ok {
		return "", false
	}
	var b strings.Builder
	b.Grow(96)
	b.WriteString("select ")
	for i := 0; i < s.nPick; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		table, name, ok := identOf(s.pickBuf[i])
		if !ok {
			return "", false
		}
		writeQual(&b, d, table, name)
	}
	b.WriteString(" from ")
	writeQual(&b, d, ft, fn)
	ph := 0
	if s.where != nil {
		left, op, _, ok := plainCompare(s.where)
		if !ok {
			return "", false
		}
		lt, ln, ok := identOf(left)
		if !ok {
			return "", false
		}
		b.WriteString(" where ")
		writeQual(&b, d, lt, ln)
		b.WriteString(op)
		b.WriteString(d.EscapeParam(ph, nil))
		ph++
	}
	if s.limitSet {
		b.WriteString(" limit ")
		b.WriteString(d.EscapeParam(ph, nil))
		ph++
	}
	if s.offsetSet {
		b.WriteString(" offset ")
		b.WriteString(d.EscapeParam(ph, nil))
	}
	return b.String(), true
}

func opCode(op string) byte {
	switch op {
	case " = ":
		return 1
	case " <> ":
		return 2
	case " > ":
		return 3
	case " >= ":
		return 4
	case " < ":
		return 5
	case " <= ":
		return 6
	case " like ":
		return 7
	case " not like ":
		return 8
	default:
		return 0
	}
}

func plainCompare(ch sql.Chunk) (sql.Chunk, string, any, bool) {
	c, ok := ch.(comparer)
	if !ok || c == nil {
		return nil, "", nil, false
	}
	left, op, right := c.Compare()
	if left == nil || op == "" || op == " ilike " || op == " not ilike " || encoded(left) || !plainValue(right) {
		return nil, "", nil, false
	}
	return left, op, right, true
}
