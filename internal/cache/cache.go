package cache

import (
	"sync"
	"time"
)

type entry[V any] struct {
	value     V
	expiresAt time.Time
}

type Cache[K comparable, V any] struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[K]entry[V]
}

func New[K comparable, V any](ttl time.Duration, now func() time.Time) *Cache[K, V] {
	return &Cache[K, V]{ttl: ttl, now: now, entries: make(map[K]entry[V])}
}

func (c *Cache[K, V]) Get(key K, loader func() (V, error)) (V, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if cached, ok := c.entries[key]; ok && c.now().Before(cached.expiresAt) {
		return cached.value, nil
	}

	value, err := loader()
	if err != nil {
		var zero V
		return zero, err
	}
	c.entries[key] = entry[V]{value: value, expiresAt: c.now().Add(c.ttl)}
	return value, nil
}
