package main

import (
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"
)

// rpTarget is the resolved upstream for a request, threaded through the request
// context into the shared ReverseProxy's Rewrite hook. The path is carried in
// both forms so the client's original percent-encoding survives to the upstream
// (decoding it would turn an encoded %2e%2e into a real traversal segment).
type rpTarget struct {
	host     string // {ipAddress}:{remotePort}
	path     string // rewritten path, decoded
	rawPath  string // rewritten path, as it will be sent on the wire
	rawquery string
}

type ctxKey struct{}

// Server routes requests to handlers, runs the auth pipeline, and forwards.
type Server struct {
	handlers []ProxyConf
	api      *APIClient
	cache    *Cache
	nb       *Notebook
	rp       *httputil.ReverseProxy

	wsMu    sync.Mutex
	wsConns map[net.Conn]struct{} // live WebSocket client conns
	wsWG    sync.WaitGroup        // tracks in-flight WS relays (incl. notebook/close)
}

func NewServer(handlers []ProxyConf, api *APIClient, cache *Cache, nb *Notebook) *Server {
	s := &Server{handlers: handlers, api: api, cache: cache, nb: nb, wsConns: make(map[net.Conn]struct{})}
	// DefaultTransport keeps only 2 idle conns per host, so a busy instance would
	// pay a TCP handshake on nearly every request.
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConnsPerHost = 64
	s.rp = &httputil.ReverseProxy{
		Transport: tr,
		Rewrite: func(pr *httputil.ProxyRequest) {
			t := pr.In.Context().Value(ctxKey{}).(*rpTarget)
			// RFC-correct forwarded headers (clean reimplementation).
			pr.SetXForwarded()
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = t.host
			pr.Out.URL.Path = t.path
			pr.Out.URL.RawPath = t.rawPath
			pr.Out.URL.RawQuery = t.rawquery
			pr.Out.Host = t.host // send the upstream's host
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("upstream error: %v", err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isWebSocketUpgrade(r) {
		s.serveWS(w, r)
		return
	}
	s.serveHTTP(w, r)
}

// match returns the first handler (in config order) whose regex matches the path.
func (s *Server) match(path string) *ProxyConf {
	for i := range s.handlers {
		if s.handlers[i].re.MatchString(path) {
			return &s.handlers[i]
		}
	}
	return nil
}

func (s *Server) trackWS(c net.Conn) {
	s.wsWG.Add(1)
	s.wsMu.Lock()
	s.wsConns[c] = struct{}{}
	s.wsMu.Unlock()
}

func (s *Server) untrackWS(c net.Conn) {
	s.wsMu.Lock()
	delete(s.wsConns, c)
	s.wsMu.Unlock()
	s.wsWG.Done()
}

// ShutdownWebSockets closes every live WebSocket client conn (unblocking its
// relay so each session fires notebook/close) and waits up to timeout for the
// relays to finish.
func (s *Server) ShutdownWebSockets(timeout time.Duration) {
	s.wsMu.Lock()
	for c := range s.wsConns {
		_ = c.Close()
	}
	s.wsMu.Unlock()
	done := make(chan struct{})
	go func() { s.wsWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for tok := range strings.SplitSeq(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(tok), "upgrade") {
			return true
		}
	}
	return false
}
