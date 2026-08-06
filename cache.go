package main

import (
	"sync"
	"time"
)

type cacheEntry struct {
	inst    *Instance
	expires time.Time
}

// Cache holds authorized instances keyed by (employeeNumber, instanceId) with a
// TTL. It deliberately does NOT coalesce concurrent misses (no singleflight):
// the API call happens outside the lock, so N concurrent misses each call the API.
type Cache struct {
	ttl time.Duration
	mu  sync.Mutex
	m   map[string]map[string]cacheEntry // employeeNumber -> instanceId -> entry
}

func NewCache(ttl time.Duration) *Cache {
	return &Cache{ttl: ttl, m: make(map[string]map[string]cacheEntry)}
}

func (c *Cache) Get(emp, id string) *Instance {
	c.mu.Lock()
	defer c.mu.Unlock()
	byInst, ok := c.m[emp]
	if !ok {
		return nil
	}
	e, ok := byInst[id]
	if !ok {
		return nil
	}
	if time.Now().After(e.expires) {
		delete(byInst, id)
		if len(byInst) == 0 {
			delete(c.m, emp)
		}
		return nil
	}
	return e.inst
}

func (c *Cache) Put(emp, id string, inst *Instance) {
	c.mu.Lock()
	defer c.mu.Unlock()
	byInst, ok := c.m[emp]
	if !ok {
		byInst = make(map[string]cacheEntry)
		c.m[emp] = byInst
	}
	byInst[id] = cacheEntry{inst: inst, expires: time.Now().Add(c.ttl)}
}

// Sweep removes all expired entries and returns how many were dropped. Entries
// are otherwise only evicted lazily on access, so without this the map grows
// unbounded as keys churn (token-hash keys rotate with the user's token).
func (c *Cache) Sweep() int {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := 0
	for emp, byInst := range c.m {
		for id, e := range byInst {
			if now.After(e.expires) {
				delete(byInst, id)
				removed++
			}
		}
		if len(byInst) == 0 {
			delete(c.m, emp)
		}
	}
	return removed
}

// StartSweeper runs Sweep every interval for the life of the process.
func (c *Cache) StartSweeper(interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		for range time.Tick(interval) {
			c.Sweep()
		}
	}()
}
