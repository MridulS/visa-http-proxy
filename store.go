package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// SessionKey identifies a persisted notebook session.
type SessionKey struct {
	InstanceID string `json:"instanceId"`
	KernelID   string `json:"kernelId"`
	SessionID  string `json:"sessionId"`
}

// Store persists notebook sessions to a single JSON file. A single mutex wraps
// the entire read-modify-write of Add/Remove, fixing the Node race that lost
// concurrent opens (its limiter serialized individual ops but not the sequence).
type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(dir string) *Store {
	_ = os.MkdirAll(dir, 0o755)
	return &Store{path: filepath.Join(dir, "instanceNotebookSessions.json")}
}

func (s *Store) readLocked() []SessionKey {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil // a missing file just means no persisted sessions
	}
	var sessions []SessionKey
	if err := json.Unmarshal(data, &sessions); err != nil {
		log.Printf("store: ignoring corrupt session file %s: %v", s.path, err)
		return nil
	}
	return sessions
}

func (s *Store) writeLocked(sessions []SessionKey) {
	data, err := json.Marshal(sessions)
	if err != nil {
		log.Printf("store: marshal failed: %v", err)
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		log.Printf("store: write %s failed: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil { // atomic replace
		log.Printf("store: rename %s -> %s failed: %v", tmp, s.path, err)
	}
}

// Add records a session. It is a set: Remove drops every copy of a key, so a
// duplicate entry (the same kernel reconnecting before the old relay tears down)
// would let that Remove un-persist a session that is still live.
func (s *Store) Add(k SessionKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessions := s.readLocked()
	if slices.Contains(sessions, k) {
		return
	}
	s.writeLocked(append(sessions, k))
}

func (s *Store) Remove(k SessionKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessions := s.readLocked()
	out := sessions[:0]
	for _, e := range sessions {
		if e != k {
			out = append(out, e)
		}
	}
	s.writeLocked(out)
}

// LoadAndClear returns all persisted sessions and empties the store atomically.
func (s *Store) LoadAndClear() []SessionKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessions := s.readLocked()
	_ = os.Remove(s.path)
	return sessions
}
