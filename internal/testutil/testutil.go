// Package testutil provides golden comparison helpers.
//
// Run the same fixture on drizzle and on mist, then compare byte for byte
// (SQL text, parameter list). A fixture only states the expected value and does not care which side produced it —
// an upstream case can become a fixture file directly, so Go does not need hand-copied expected strings.
//
// Run walks a directory of JSON fixtures. Each Case.Name matches one file.
// `go test -update-golden` rewrites those files from the builders.
package testutil

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/xbmlz/mist/sql"
)

// updateGolden rewrites fixture JSON from the current query.
// Add a Case, then run: go test -update-golden
var updateGolden = flag.Bool("update-golden", false, "rewrite golden JSON fixtures from the current SQL")

// Case is one golden query. Name is the fixture file name without ".json".
//
// To add a case: append a Case, run `go test -update-golden` in that package,
// and commit the JSON file the command writes. A JSON file with no Case, or a
// Case with no JSON file, fails the test.
type Case struct {
	Name  string
	Build func() (sql.Query, error)
}

// Run compares every Case with dir/<Name>.json.
func Run(t *testing.T, dir string, cases []Case) {
	t.Helper()
	byName := make(map[string]Case, len(cases))
	for _, c := range cases {
		if c.Name == "" || c.Build == nil {
			t.Fatalf("testutil: case %q needs a name and a builder", c.Name)
		}
		if _, ok := byName[c.Name]; ok {
			t.Fatalf("testutil: duplicate case %q", c.Name)
		}
		byName[c.Name] = c
	}

	entries, err := os.ReadDir(dir)
	if err != nil && !*updateGolden {
		t.Fatalf("testutil: read %s: %v", dir, err)
	}
	files := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		files[strings.TrimSuffix(entry.Name(), ".json")] = struct{}{}
	}
	for name := range files {
		if _, ok := byName[name]; !ok {
			t.Errorf("testutil: fixture %s.json has no case", name)
		}
	}
	if !*updateGolden {
		for name := range byName {
			if _, ok := files[name]; !ok {
				t.Errorf("testutil: case %s has no fixture; run go test -update-golden", name)
			}
		}
	}
	if t.Failed() {
		return
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			got, err := byName[name].Build()
			if err != nil {
				t.Fatalf("testutil: build %s: %v", name, err)
			}
			path := filepath.Join(dir, name+".json")
			if *updateGolden {
				if err := writeFixture(path, got); err != nil {
					t.Fatal(err)
				}
				return
			}
			AssertQuery(t, got, LoadFixture(t, path))
		})
	}
}

func writeFixture(path string, got sql.Query) error {
	params := got.Params
	if params == nil {
		params = []any{}
	}
	out, err := json.MarshalIndent(struct {
		SQL    string `json:"sql"`
		Params []any  `json:"params"`
	}{SQL: got.SQL, Params: params}, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o644)
}

// Fixture is one golden expectation.
//
// The SQL text must match byte for byte. Params are compared as canonical JSON, avoiding small
// differences in how Go and JS spell numbers (for example 1 and 1.0).
type Fixture struct {
	SQL    string          `json:"sql"`
	Params json.RawMessage `json:"params"`
}

// LoadFixture reads a fixture file. A parse failure stops the test.
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

// AssertQuery asserts that the rendered result matches the fixture.
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

	// nil and an empty slice mean the same thing. Encode both as [] before comparing,
	// otherwise nil becomes null and does not match [] in the fixture.
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

// JSONEqual reports whether two JSON texts are equal after canonicalization.
func JSONEqual(t testing.TB, a, b []byte) bool {
	t.Helper()
	return canonicalJSON(t, a) == canonicalJSON(t, b)
}

// canonicalJSON decodes JSON and encodes it again, dropping differences in whitespace, key order, and number literals.
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
