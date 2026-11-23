package relation

import "testing"

func TestShouldWrap(t *testing.T) {
	cases := []struct {
		name                      string
		strategy                  string
		with                      int
		rel, limit, offset, order bool
		want                      bool
	}{
		{"basic", StrategyBasic, 1, true, true, true, true, false},
		{"no with", StrategyOptimized, 0, true, true, true, true, false},
		{"plain with", StrategyOptimized, 1, false, false, false, false, false},
		{"limit without order", StrategyOptimized, 1, false, true, false, false, false},
		{"limit and order", StrategyOptimized, 1, false, true, false, true, true},
		{"offset", StrategyOptimized, 1, false, false, true, false, true},
		{"relation filter", StrategyOptimized, 1, true, false, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ShouldWrap(tc.strategy, tc.with, tc.rel, tc.limit, tc.offset, tc.order)
			if got != tc.want {
				t.Fatalf("ShouldWrap() = %v, want %v", got, tc.want)
			}
		})
	}
}
