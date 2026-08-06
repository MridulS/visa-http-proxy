package main

import (
	"log"
	"regexp"
)

// sessionRe matches a Jupyter kernel WebSocket URL and captures
// (instanceId, kernelId, sessionId). session_id lives in the query string, so
// this is matched against the full request URI.
var sessionRe = regexp.MustCompile(`^/jupyter/(\d+)/api/kernels/([a-f0-9-]+).*session_id=([a-f0-9-]+).*$`)

// Notebook orchestrates the Jupyter notebook session lifecycle.
type Notebook struct {
	api   *APIClient
	store *Store
}

func NewNotebook(api *APIClient, store *Store) *Notebook {
	return &Notebook{api: api, store: store}
}

// parseSession extracts the session key from a Jupyter kernel WS request URI.
func parseSession(requestURI string) (SessionKey, bool) {
	m := sessionRe.FindStringSubmatch(requestURI)
	if m == nil {
		return SessionKey{}, false
	}
	return SessionKey{InstanceID: m[1], KernelID: m[2], SessionID: m[3]}, true
}

// OnOpen persists the session and notifies the API (with auth). A failed
// notebook/open is logged and swallowed — the WebSocket still proceeds (CLEAN).
func (n *Notebook) OnOpen(k SessionKey, token string) {
	n.store.Add(k)
	if err := n.api.NotebookOpen(k.InstanceID, k.KernelID, k.SessionID, token); err != nil {
		log.Printf("notebook/open failed (swallowed): %v", err)
	}
}

// OnClose notifies the API (with the token captured at open) and removes the
// persisted session. A failed notebook/close is logged and swallowed.
func (n *Notebook) OnClose(k SessionKey, token string) {
	if err := n.api.NotebookClose(k.InstanceID, k.KernelID, k.SessionID, token); err != nil {
		log.Printf("notebook/close failed (swallowed): %v", err)
	}
	n.store.Remove(k)
}

// CleanupZombies closes sessions persisted by a previous run. No token is
// available after a restart, so these closes carry no Authorization header.
func (n *Notebook) CleanupZombies() {
	for _, k := range n.store.LoadAndClear() {
		log.Printf("erasing zombie notebook session kernel=%s session=%s", k.KernelID, k.SessionID)
		if err := n.api.NotebookClose(k.InstanceID, k.KernelID, k.SessionID, ""); err != nil {
			log.Printf("zombie notebook/close failed (swallowed): %v", err)
		}
	}
}
