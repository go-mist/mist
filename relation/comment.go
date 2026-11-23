package relation

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// FormatComment builds an sqlcommenter comment. An empty result is an empty string, and the caller must not write it into SQL.
// Comment delimiters inside the string are split apart. Tags are sorted by key, and values use encodeURIComponent.
func FormatComment(text string, tags map[string]string) string {
	var parts []string
	if text != "" {
		parts = append(parts, sanitizeComment(text))
	}
	if encoded := encodeTags(tags); encoded != "" {
		parts = append(parts, encoded)
	}
	if len(parts) == 0 {
		return ""
	}
	return "/*" + strings.Join(parts, ",") + "*/"
}

func sanitizeComment(s string) string {
	s = strings.ReplaceAll(s, "/*", "/ *")
	s = strings.ReplaceAll(s, "*/", "* /")
	return s
}

func encodeTags(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for key, value := range tags {
		if value == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, encodeURIComponent(key)+"='"+strings.ReplaceAll(encodeURIComponent(tags[key]), "'", `\'`)+"'")
	}
	return strings.Join(parts, ",")
}

func encodeURIComponent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if isURIUnreserved(r) {
			b.WriteRune(r)
			continue
		}
		var buf [utf8.UTFMax]byte
		n := utf8.EncodeRune(buf[:], r)
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "%%%02X", buf[i])
		}
	}
	return b.String()
}

func isURIUnreserved(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	switch r {
	case '-', '_', '.', '!', '~', '*', '\'', '(', ')':
		return true
	default:
		return false
	}
}
