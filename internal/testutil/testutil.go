// Package testutil 提供 golden 对拍工具。
//
// 对拍的思路：同一份 fixture 在 drizzle 与 drizzle-go 上跑，逐字节比较
// (SQL 文本, 参数列表)。fixture 只描述期望值，不关心由哪一侧产出 ——
// 上游用例可以直接转成 fixture 文件，Go 侧不需要手工抄期望字符串。
package testutil

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/xbmlz/drizzle-go/sql"
)

// Fixture 是一份 golden 期望。
//
// SQL 文本必须逐字节相等；Params 用规范化 JSON 比较，以避开 Go 与 JS
// 在数字表示上的细微差异（例如 1 与 1.0）。
type Fixture struct {
	SQL    string          `json:"sql"`
	Params json.RawMessage `json:"params"`
}

// LoadFixture 读取一个 fixture 文件；解析失败直接终止测试。
func LoadFixture(t testing.TB, path string) Fixture {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("testutil: read fixture %s: %v", path, err)
	}
	var f Fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("testutil: parse fixture %s: %v", path, err)
	}
	return f
}

// AssertQuery 断言渲染结果与 fixture 一致。
func AssertQuery(t testing.TB, got sql.Query, want Fixture) {
	t.Helper()

	if got.SQL != want.SQL {
		t.Fatalf("testutil: SQL mismatch\n got: %s\nwant: %s", got.SQL, want.SQL)
	}

	if want.Params == nil {
		if len(got.Params) != 0 {
			t.Fatalf("testutil: expected no params, got %#v", got.Params)
		}
		return
	}

	// nil 与空切片在语义上相同；统一编码成 [] 再比较，
	// 否则 nil 会编码成 null，与 fixture 里的 [] 不等。
	params := got.Params
	if params == nil {
		params = []any{}
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("testutil: marshal params: %v", err)
	}
	if !JSONEqual(t, encoded, want.Params) {
		t.Fatalf("testutil: params mismatch\n got: %s\nwant: %s",
			canonicalJSON(t, encoded), canonicalJSON(t, want.Params))
	}
}

// JSONEqual 判断两段 JSON 是否在规范化之后相等。
func JSONEqual(t testing.TB, a, b []byte) bool {
	t.Helper()
	return canonicalJSON(t, a) == canonicalJSON(t, b)
}

// canonicalJSON 把 JSON 解码再编码，消除空白、键序与数字字面写法差异。
func canonicalJSON(t testing.TB, raw []byte) string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("testutil: decode JSON %s: %v", raw, err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("testutil: encode JSON %v: %v", v, err)
	}
	return string(out)
}
