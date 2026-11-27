// Package id generates values the caller passes to Insert.
//
// Snowflake is an int64. NewUUID is a random UUID string. Neither value is written into SQL by this package.
package id

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"
)

// snowflakeEpoch is 2020-01-01T00:00:00Z in milliseconds.
const snowflakeEpoch int64 = 1577836800000

const (
	snowflakeSeqBits = 12
	snowflakeNodeMax = 1<<10 - 1
	snowflakeSeqMax  = 1<<snowflakeSeqBits - 1
)

// Snowflake generates int64 identifiers.
// Each value is 41 bits of milliseconds since 2020-01-01 UTC, 10 bits of node, and 12 bits of sequence.
type Snowflake struct {
	mu   sync.Mutex
	node int64
	last int64
	seq  int64
	now  func() int64
}

// NewSnowflake returns a generator for node. node must be 0 through 1023.
func NewSnowflake(node int64) (*Snowflake, error) {
	if node < 0 || node > snowflakeNodeMax {
		return nil, errors.New("id: snowflake node must be 0 through 1023")
	}
	return &Snowflake{node: node}, nil
}

func (s *Snowflake) millis() int64 {
	if s.now != nil {
		return s.now()
	}
	return time.Now().UnixMilli()
}

// Next returns the next identifier.
func (s *Snowflake) Next() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ms := s.millis()
	if ms < s.last {
		return 0, errors.New("id: clock moved backwards")
	}
	if ms == s.last {
		s.seq++
		if s.seq > snowflakeSeqMax {
			for ms <= s.last {
				time.Sleep(time.Millisecond)
				ms = s.millis()
			}
			s.seq = 0
		}
	} else {
		s.seq = 0
	}
	s.last = ms
	ts := ms - snowflakeEpoch
	if ts < 0 {
		return 0, errors.New("id: time is before snowflake epoch")
	}
	if ts >= 1<<41 {
		return 0, errors.New("id: snowflake time overflow")
	}
	return (ts << 22) | (s.node << 12) | s.seq, nil
}

// NewUUID returns a random UUID version 4 string.
func NewUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
