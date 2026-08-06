package main

import (
	"testing"
	"time"
)

func TestCacheSweepRemovesExpired(t *testing.T) {
	c := NewCache(-time.Second) // entries expire in the past
	c.Put("u1", "10", &Instance{IP: "1.1.1.1"})
	c.Put("u2", "20", &Instance{IP: "2.2.2.2"})
	if got := c.Sweep(); got != 2 {
		t.Fatalf("Sweep dropped %d, want 2", got)
	}
	if len(c.m) != 0 {
		t.Fatalf("map should be empty after sweeping all-expired, got %v", c.m)
	}
}

func TestCacheSweepKeepsLive(t *testing.T) {
	c := NewCache(time.Hour)
	c.Put("u1", "10", &Instance{IP: "1.1.1.1"})
	if got := c.Sweep(); got != 0 {
		t.Fatalf("Sweep dropped %d live entries, want 0", got)
	}
	if c.Get("u1", "10") == nil {
		t.Fatal("live entry was evicted by Sweep")
	}
}
