package relation

import (
	"context"
	"sync"
)

// Cache remembers the result of one FindMany or FindFirst.
// It does not watch table changes. The caller decides when to drop an old value.
type Cache interface {
	Get(ctx context.Context, key string) (data []byte, ok bool, err error)
	Set(ctx context.Context, key string, data []byte) error
}

// MemoryCache is an in-process Cache.
type MemoryCache struct {
	mu sync.Mutex
	m  map[string][]byte
}

// Get returns a copy.
func (c *MemoryCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.m[key]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), data...), true, nil
}

// Set stores a copy.
func (c *MemoryCache) Set(_ context.Context, key string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string][]byte)
	}
	c.m[key] = append([]byte(nil), data...)
	return nil
}
