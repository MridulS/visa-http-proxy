package main

import "testing"

// The store is a set. A kernel that reconnects before its old relay tears down
// adds the same key twice; since Remove drops every copy, a duplicate would let
// the first close un-persist a session that is still live — and a crash after
// that would leave it open in the API with nothing to reconcile it.
func TestStoreAddIsIdempotent(t *testing.T) {
	s := NewStore(t.TempDir())
	k := SessionKey{InstanceID: "1", KernelID: "kern-a", SessionID: "sess-a"}
	other := SessionKey{InstanceID: "1", KernelID: "kern-b", SessionID: "sess-b"}

	s.Add(k)
	s.Add(k)
	s.Add(other)
	s.Remove(k)

	if got := s.LoadAndClear(); len(got) != 1 || got[0] != other {
		t.Fatalf("after Add(k) x2, Add(other), Remove(k): %v, want [%v]", got, other)
	}
}
