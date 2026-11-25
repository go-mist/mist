package sqlite

import (
	"strings"
	"sync"

	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/sql"
)

// RenderSelect writes a simple select directly. The text matches assembleSelect plus Render.
// A complicated statement returns ok false and the caller uses the general renderer.
// Repeated shapes reuse the SQL text. Parameters are still bound on every call.
func (Dialect) RenderSelect(spec query.SelectSpec) (sql.Query, bool, error) {
	if q, ok := lookupSelect(spec); ok {
		return q, true, nil
	}
	return writeSelect(spec)
}

func writeSelect(spec query.SelectSpec) (sql.Query, bool, error) {
	if spec.SetErr != "" || len(spec.With) > 0 || len(spec.Sets) > 0 || len(spec.Joins) > 0 ||
		len(spec.GroupBy) > 0 || spec.Having != nil || len(spec.DistinctOn) > 0 || spec.Lock != nil ||
		spec.TopSet || spec.From == nil || len(spec.Columns) == 0 {
		return sql.Query{}, false, nil
	}
	w := borrowWriter()
	defer w.release()
	if spec.Distinct {
		w.b.WriteString("select distinct ")
	} else {
		w.b.WriteString("select ")
	}
	for i, col := range spec.Columns {
		if i > 0 {
			w.b.WriteString(", ")
		}
		if !w.chunk(col) {
			return sql.Query{}, false, nil
		}
	}
	w.b.WriteString(" from ")
	if !w.chunk(spec.From) {
		return sql.Query{}, false, nil
	}
	if spec.Where != nil {
		w.b.WriteString(" where ")
		if !w.chunk(spec.Where) {
			return sql.Query{}, false, nil
		}
	}
	if len(spec.OrderBy) > 0 {
		w.b.WriteString(" order by ")
		for i, expr := range spec.OrderBy {
			if i > 0 {
				w.b.WriteString(", ")
			}
			if !w.chunk(expr) {
				return sql.Query{}, false, nil
			}
		}
	}
	if spec.LimitSet {
		w.b.WriteString(" limit ?")
		w.params = append(w.params, spec.Limit)
	}
	if spec.OffsetSet {
		w.b.WriteString(" offset ?")
		w.params = append(w.params, spec.Offset)
	}
	return sql.Query{SQL: w.b.String(), Params: w.params}, true, nil
}

// RenderInsert writes a simple insert directly. The text matches BuildInsertQuery plus Render.
func (Dialect) RenderInsert(spec query.InsertSpec) (sql.Query, bool, error) {
	if q, ok := lookupInsert(spec); ok {
		return q, true, nil
	}
	return writeInsert(spec)
}

func writeInsert(spec query.InsertSpec) (sql.Query, bool, error) {
	if spec.DuplicateKey || spec.Conflict != nil || spec.Select != nil || len(spec.With) > 0 || len(spec.Returning) > 0 || spec.HasOutput || len(spec.Output) > 0 {
		return sql.Query{}, false, nil
	}
	if spec.Table == nil || len(spec.ColumnNames) == 0 || len(spec.Rows) == 0 {
		return sql.Query{}, false, nil
	}
	for _, enc := range spec.Encoders {
		if enc != nil && !enc.IsNoop() {
			return sql.Query{}, false, nil
		}
	}
	for _, row := range spec.Rows {
		if len(row) != len(spec.ColumnNames) {
			return sql.Query{}, false, nil
		}
		for _, cell := range row {
			if _, ok := cell.(sql.Chunk); ok {
				return sql.Query{}, false, nil
			}
		}
	}
	w := borrowWriter()
	defer w.release()
	if spec.Ignore {
		w.b.WriteString("insert or ignore into ")
	} else {
		w.b.WriteString("insert into ")
	}
	if !w.chunk(spec.Table) {
		return sql.Query{}, false, nil
	}
	w.b.WriteString(" (")
	for i, name := range spec.ColumnNames {
		if name == "" {
			return sql.Query{}, false, nil
		}
		if i > 0 {
			w.b.WriteString(", ")
		}
		writeIdent(w.b, name)
	}
	w.b.WriteString(") values ")
	for r, row := range spec.Rows {
		if r > 0 {
			w.b.WriteString(", ")
		}
		w.b.WriteByte('(')
		for i, cell := range row {
			if i > 0 {
				w.b.WriteString(", ")
			}
			w.params = append(w.params, cell)
			w.b.WriteByte('?')
		}
		w.b.WriteByte(')')
	}
	return sql.Query{SQL: w.b.String(), Params: w.params}, true, nil
}

// RenderUpdate writes a simple update directly. The text matches BuildUpdateQuery plus Render.
func (Dialect) RenderUpdate(spec query.UpdateSpec) (sql.Query, bool, error) {
	if q, ok := lookupUpdate(spec); ok {
		return q, true, nil
	}
	return writeUpdate(spec)
}

func writeUpdate(spec query.UpdateSpec) (sql.Query, bool, error) {
	if len(spec.With) > 0 || len(spec.Returning) > 0 || spec.HasOutputInserted || spec.HasOutputDeleted ||
		len(spec.OutputInserted) > 0 || len(spec.OutputDeleted) > 0 || spec.From != nil || len(spec.Joins) > 0 ||
		len(spec.OrderBy) > 0 || spec.LimitSet || spec.Table == nil || len(spec.Sets) == 0 {
		return sql.Query{}, false, nil
	}
	w := borrowWriter()
	defer w.release()
	w.b.WriteString("update ")
	if !w.chunk(spec.Table) {
		return sql.Query{}, false, nil
	}
	w.b.WriteString(" set ")
	for i, set := range spec.Sets {
		if set.Column == "" || (set.Encoder != nil && !set.Encoder.IsNoop()) {
			return sql.Query{}, false, nil
		}
		if i > 0 {
			w.b.WriteString(", ")
		}
		writeIdent(w.b, set.Column)
		w.b.WriteString(" = ")
		if ch, ok := set.Value.(sql.Chunk); ok && ch != nil {
			if !w.chunk(ch) {
				return sql.Query{}, false, nil
			}
			continue
		}
		w.params = append(w.params, set.Value)
		w.b.WriteByte('?')
	}
	if spec.Where != nil {
		w.b.WriteString(" where ")
		if !w.chunk(spec.Where) {
			return sql.Query{}, false, nil
		}
	}
	return sql.Query{SQL: w.b.String(), Params: w.params}, true, nil
}

type sqlWriter struct {
	b      *strings.Builder
	params []any
}

var writerPool = sync.Pool{New: func() any {
	b := new(strings.Builder)
	b.Grow(128)
	return &sqlWriter{b: b}
}}

func borrowWriter() *sqlWriter {
	w := writerPool.Get().(*sqlWriter)
	w.b.Reset()
	w.params = make([]any, 0, 4)
	return w
}

func (w *sqlWriter) release() {
	if w.b.Cap() > 4096 {
		return
	}
	w.params = nil
	writerPool.Put(w)
}

type comparer interface {
	Compare() (sql.Chunk, string, any)
}

type director interface {
	Direction() (sql.Chunk, string)
}

type identifier interface {
	Ident() (table, name string)
}

func (w *sqlWriter) chunk(ch sql.Chunk) bool {
	if ch == nil {
		return true
	}
	switch v := ch.(type) {
	case sql.Fragment:
		if v == " ilike " || v == " not ilike " {
			return false
		}
		w.b.WriteString(string(v))
		return true
	case sql.Name:
		writeIdent(w.b, string(v))
		return true
	case *sql.Param:
		return w.param(v)
	case *sql.Placeholder:
		w.params = append(w.params, v)
		w.b.WriteByte('?')
		return true
	case *sql.SQL:
		if v == nil {
			return true
		}
		if v.ParamsInlined() {
			return false
		}
		for _, c := range v.QueryChunks {
			if !w.chunk(c) {
				return false
			}
		}
		return true
	}
	if c, ok := ch.(comparer); ok {
		left, op, right := c.Compare()
		return w.cmp(left, op, right)
	}
	if s, ok := ch.(director); ok {
		expr, dir := s.Direction()
		if !w.chunk(expr) {
			return false
		}
		w.b.WriteString(dir)
		return true
	}
	if id, ok := ch.(identifier); ok {
		table, name := id.Ident()
		writeQual(w.b, table, name)
		return true
	}
	return false
}

func (w *sqlWriter) cmp(left sql.Chunk, op string, right any) bool {
	if op == "" || op == " ilike " || op == " not ilike " || coded(left) {
		return false
	}
	if !w.chunk(left) {
		return false
	}
	w.b.WriteString(op)
	if ch, ok := right.(sql.Chunk); ok && ch != nil {
		return w.chunk(ch)
	}
	w.params = append(w.params, right)
	w.b.WriteByte('?')
	return true
}

func (w *sqlWriter) param(p *sql.Param) bool {
	if p == nil {
		return true
	}
	if p.Encoder != nil && !p.Encoder.IsNoop() {
		return false
	}
	switch v := p.Value.(type) {
	case *sql.SQL:
		return false
	case *sql.Placeholder:
		w.params = append(w.params, v)
		w.b.WriteByte('?')
		return true
	default:
		w.params = append(w.params, p.Value)
		w.b.WriteByte('?')
		return true
	}
}

func coded(ch sql.Chunk) bool {
	e, ok := ch.(interface{ DriverEncoder() sql.Encoder })
	if !ok || e == nil {
		return false
	}
	enc := e.DriverEncoder()
	return enc != nil && !enc.IsNoop()
}

func writeQual(b *strings.Builder, table, name string) {
	if table != "" {
		writeIdent(b, table)
		b.WriteByte('.')
	}
	writeIdent(b, name)
}

func writeIdent(b *strings.Builder, name string) {
	b.WriteByte('"')
	for i := 0; i < len(name); i++ {
		if name[i] == '"' {
			b.WriteString(`""`)
			continue
		}
		b.WriteByte(name[i])
	}
	b.WriteByte('"')
}

// shapeKey identifies one simple statement. Pointer identities stay stable for table and column variables.
// A value copied into an interface is not cached, so the map cannot grow with every call.
type shapeKey struct {
	kind  byte
	flags uint32
	p     [8]uintptr
	s0    string
	s1    string
	s2    string
	s3    string
	s4    string
	s5    string
}

const shapeCacheMax = 64

var (
	shapeMu  sync.Mutex
	shapeN   int
	shapeSQL map[shapeKey]string
)

func remember(k shapeKey, build func() (string, bool)) (string, bool) {
	shapeMu.Lock()
	if shapeSQL != nil {
		if s, ok := shapeSQL[k]; ok {
			shapeMu.Unlock()
			return s, true
		}
	}
	shapeMu.Unlock()
	s, ok := build()
	if !ok {
		return "", false
	}
	shapeMu.Lock()
	if shapeSQL == nil {
		shapeSQL = make(map[shapeKey]string)
	}
	if prev, exists := shapeSQL[k]; exists {
		s = prev
	} else if shapeN < shapeCacheMax {
		shapeSQL[k] = s
		shapeN++
	}
	shapeMu.Unlock()
	return s, true
}

func ptrOf(ch sql.Chunk) (uintptr, bool) {
	s, ok := ch.(interface{ StablePtr() uintptr })
	if !ok || s == nil {
		return 0, false
	}
	p := s.StablePtr()
	if p == 0 {
		return 0, false
	}
	return p, true
}

func lookupSelect(spec query.SelectSpec) (sql.Query, bool) {
	k, params, ok := selectKey(spec)
	if !ok {
		return sql.Query{}, false
	}
	text, ok := remember(k, func() (string, bool) {
		q, done, _ := writeSelect(spec)
		if !done {
			return "", false
		}
		return q.SQL, true
	})
	if !ok {
		return sql.Query{}, false
	}
	return sql.Query{SQL: text, Params: params}, true
}

func selectKey(spec query.SelectSpec) (shapeKey, []any, bool) {
	var k shapeKey
	if spec.SetErr != "" || len(spec.With) > 0 || len(spec.Sets) > 0 || len(spec.Joins) > 0 ||
		len(spec.GroupBy) > 0 || spec.Having != nil || len(spec.DistinctOn) > 0 || spec.Lock != nil ||
		spec.TopSet || spec.From == nil || len(spec.Columns) == 0 || len(spec.Columns) > 4 || len(spec.OrderBy) > 2 {
		return k, nil, false
	}
	var ok bool
	if k.p[0], ok = ptrOf(spec.From); !ok {
		return k, nil, false
	}
	k.kind = 's'
	for i, col := range spec.Columns {
		if k.p[1+i], ok = ptrOf(col); !ok {
			return k, nil, false
		}
	}
	k.flags = uint32(len(spec.Columns))
	if spec.Distinct {
		k.flags |= 1 << 8
	}
	params := make([]any, 0, 4)
	if spec.Where != nil {
		c, isCmp := spec.Where.(comparer)
		if !isCmp {
			return k, nil, false
		}
		left, op, right := c.Compare()
		if op == "" || op == " ilike " || op == " not ilike " || coded(left) {
			return k, nil, false
		}
		if _, isCh := right.(sql.Chunk); isCh && right != nil {
			return k, nil, false
		}
		if k.p[5], ok = ptrOf(left); !ok {
			return k, nil, false
		}
		k.s0 = op
		params = append(params, right)
	}
	for i, expr := range spec.OrderBy {
		d, isDir := expr.(director)
		if !isDir {
			return k, nil, false
		}
		inner, dir := d.Direction()
		p, ok := ptrOf(inner)
		if !ok {
			return k, nil, false
		}
		k.p[6+i] = p
		if i == 0 {
			k.s1 = dir
		} else {
			k.s2 = dir
		}
	}
	if spec.LimitSet {
		k.flags |= 1 << 9
		params = append(params, spec.Limit)
	}
	if spec.OffsetSet {
		k.flags |= 1 << 10
		params = append(params, spec.Offset)
	}
	return k, params, true
}

func lookupInsert(spec query.InsertSpec) (sql.Query, bool) {
	k, params, ok := insertKey(spec)
	if !ok {
		return sql.Query{}, false
	}
	text, ok := remember(k, func() (string, bool) {
		q, done, _ := writeInsert(spec)
		if !done {
			return "", false
		}
		return q.SQL, true
	})
	if !ok {
		return sql.Query{}, false
	}
	return sql.Query{SQL: text, Params: params}, true
}

func insertKey(spec query.InsertSpec) (shapeKey, []any, bool) {
	var k shapeKey
	if spec.DuplicateKey || spec.Conflict != nil || spec.Select != nil || len(spec.With) > 0 || len(spec.Returning) > 0 || spec.HasOutput || len(spec.Output) > 0 {
		return k, nil, false
	}
	if spec.Table == nil || len(spec.ColumnNames) == 0 || len(spec.ColumnNames) > 4 || len(spec.Rows) == 0 || len(spec.Rows) > 4 {
		return k, nil, false
	}
	for _, enc := range spec.Encoders {
		if enc != nil && !enc.IsNoop() {
			return k, nil, false
		}
	}
	var ok bool
	if k.p[0], ok = ptrOf(spec.Table); !ok {
		return k, nil, false
	}
	k.kind = 'i'
	k.flags = uint32(len(spec.Rows))
	if spec.Ignore {
		k.flags |= 1 << 8
	}
	names := []*string{&k.s0, &k.s1, &k.s2, &k.s3}
	for i, name := range spec.ColumnNames {
		if name == "" {
			return k, nil, false
		}
		*names[i] = name
	}
	params := make([]any, 0, len(spec.Rows)*len(spec.ColumnNames))
	for _, row := range spec.Rows {
		if len(row) != len(spec.ColumnNames) {
			return k, nil, false
		}
		for _, cell := range row {
			if _, isCh := cell.(sql.Chunk); isCh && cell != nil {
				return k, nil, false
			}
			params = append(params, cell)
		}
	}
	return k, params, true
}

func lookupUpdate(spec query.UpdateSpec) (sql.Query, bool) {
	k, params, ok := updateKey(spec)
	if !ok {
		return sql.Query{}, false
	}
	text, ok := remember(k, func() (string, bool) {
		q, done, _ := writeUpdate(spec)
		if !done {
			return "", false
		}
		return q.SQL, true
	})
	if !ok {
		return sql.Query{}, false
	}
	return sql.Query{SQL: text, Params: params}, true
}

func updateKey(spec query.UpdateSpec) (shapeKey, []any, bool) {
	var k shapeKey
	if len(spec.With) > 0 || len(spec.Returning) > 0 || spec.HasOutputInserted || spec.HasOutputDeleted ||
		len(spec.OutputInserted) > 0 || len(spec.OutputDeleted) > 0 || spec.From != nil || len(spec.Joins) > 0 ||
		len(spec.OrderBy) > 0 || spec.LimitSet || spec.Table == nil || len(spec.Sets) == 0 || len(spec.Sets) > 4 {
		return k, nil, false
	}
	var ok bool
	if k.p[0], ok = ptrOf(spec.Table); !ok {
		return k, nil, false
	}
	k.kind = 'u'
	k.flags = uint32(len(spec.Sets))
	names := []*string{&k.s0, &k.s1, &k.s2, &k.s3}
	params := make([]any, 0, len(spec.Sets)+1)
	for i, set := range spec.Sets {
		if set.Column == "" || (set.Encoder != nil && !set.Encoder.IsNoop()) {
			return k, nil, false
		}
		if ch, isCh := set.Value.(sql.Chunk); isCh && ch != nil {
			return k, nil, false
		}
		*names[i] = set.Column
		params = append(params, set.Value)
	}
	if spec.Where != nil {
		c, isCmp := spec.Where.(comparer)
		if !isCmp {
			return k, nil, false
		}
		left, op, right := c.Compare()
		if op == "" || op == " ilike " || op == " not ilike " || coded(left) {
			return k, nil, false
		}
		if _, isCh := right.(sql.Chunk); isCh && right != nil {
			return k, nil, false
		}
		if k.p[5], ok = ptrOf(left); !ok {
			return k, nil, false
		}
		k.s4 = op
		params = append(params, right)
	}
	return k, params, true
}
