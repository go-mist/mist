package relation

import "testing"

func TestFormatComment(t *testing.T) {
	if got := FormatComment("", nil); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := FormatComment("trace", nil); got != "/*trace*/" {
		t.Fatalf("text = %q", got)
	}
	if got := FormatComment("a /* b */ c", nil); got != "/*a / * b * / c*/" {
		t.Fatalf("sanitized = %q", got)
	}
	got := FormatComment("", map[string]string{"route": "a'b", "app": "demo", "skip": ""})
	want := `/*app='demo',route='a\'b'*/`
	if got != want {
		t.Fatalf("tags = %q", got)
	}
	got = FormatComment("trace", map[string]string{"app": "demo"})
	if got != `/*trace,app='demo'*/` {
		t.Fatalf("both = %q", got)
	}
}
