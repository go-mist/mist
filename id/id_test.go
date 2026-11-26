package id

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSnowflakeNode(t *testing.T) {
	if _, err := NewSnowflake(-1); err == nil {
		t.Fatal("negative node was accepted")
	}
	if _, err := NewSnowflake(1024); err == nil {
		t.Fatal("node 1024 was accepted")
	}
	s, err := NewSnowflake(7)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("duplicate id %d", a)
	}
	if a < 0 || b < 0 {
		t.Fatalf("negative ids %d %d", a, b)
	}
}

func TestSnowflakeClock(t *testing.T) {
	s, err := NewSnowflake(1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	s.now = func() int64 { return now }
	if _, err := s.Next(); err != nil {
		t.Fatal(err)
	}
	s.now = func() int64 { return now - 5 }
	if _, err := s.Next(); err == nil {
		t.Fatal("clock moving backwards was accepted")
	}
}

func TestSnowflakeSequenceWaits(t *testing.T) {
	s, err := NewSnowflake(2)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.now = func() int64 {
		calls++
		if calls < 3 {
			return snowflakeEpoch + 1000
		}
		return snowflakeEpoch + 1001
	}
	s.last = snowflakeEpoch + 1000
	s.seq = snowflakeSeqMax
	got, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if got&snowflakeSeqMax != 0 {
		t.Fatalf("sequence = %d", got&snowflakeSeqMax)
	}
	if s.last != snowflakeEpoch+1001 {
		t.Fatalf("last = %d", s.last)
	}
}

func TestSnowflakeConcurrent(t *testing.T) {
	s, err := NewSnowflake(3)
	if err != nil {
		t.Fatal(err)
	}
	const n = 200
	ids := make([]int64, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func() {
			defer wg.Done()
			id, err := s.Next()
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = id
		}()
	}
	wg.Wait()
	seen := make(map[int64]struct{}, n)
	for _, id := range ids {
		if id == 0 {
			t.Fatal("missing id")
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate %d", id)
		}
		seen[id] = struct{}{}
	}
}

func TestNewUUID(t *testing.T) {
	a, err := NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("duplicate uuid %s", a)
	}
	if len(a) != 36 || a[14] != '4' {
		t.Fatalf("uuid = %s", a)
	}
	parts := strings.Split(a, "-")
	if len(parts) != 5 {
		t.Fatalf("uuid = %s", a)
	}
}
