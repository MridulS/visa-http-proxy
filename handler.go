package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

// serveHTTP runs the HTTP request pipeline: match -> resolve -> forward.
func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	h := s.match(r.URL.EscapedPath())
	if h == nil {
		http.NotFound(w, r) // CLEAN: no handler -> 404 (Node hung)
		return
	}
	target, status, msg := s.resolve(r, h)
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	ctx := context.WithValue(r.Context(), ctxKey{}, target)
	s.rp.ServeHTTP(w, r.WithContext(ctx))
}

// resolve performs the auth + instance-resolution shared by HTTP and WS.
// Returns (target, 0, "") on success, or (nil, status, body) on failure.
func (s *Server) resolve(r *http.Request, h *ProxyConf) (*rpTarget, int, string) {
	token, ok := ExtractToken(r)
	if !ok {
		return nil, http.StatusUnauthorized, "No access token"
	}
	// The token is NOT validated locally — the VISA API is the authority; a bad
	// token is forwarded to the API, which rejects it (propagated below). The cache
	// key is a hash of the whole token (see userCacheKey), so a cache hit can only
	// be produced by the genuine token holder — a forged claim can't reproduce it.
	emp := userCacheKey(token)
	// Match and rewrite the path as it arrived on the wire, not decoded: an
	// encoded %2e%2e must stay encoded rather than become a traversal segment.
	escaped := r.URL.EscapedPath()
	id := firstCapture(h.re, escaped)
	if id == "" {
		return nil, http.StatusNotFound, "Instance id not present in the path"
	}
	inst, status := s.lookupInstance(r.Context(), emp, id, token)
	if status != 0 {
		return nil, status, http.StatusText(status)
	}
	if len(h.PathRewrite) > 0 {
		escaped = h.PathRewrite.Apply(escaped)
	}
	decoded, err := url.PathUnescape(escaped)
	if err != nil { // only reachable via a pathRewrite replacement with a stray '%'
		log.Printf("handler %q: rewritten path %q is not valid percent-encoding: %v", h.Name, escaped, err)
		return nil, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError)
	}
	return &rpTarget{
		host:     net.JoinHostPort(inst.IP, strconv.Itoa(h.RemotePort)),
		path:     decoded,
		rawPath:  escaped,
		rawquery: r.URL.RawQuery,
	}, 0, ""
}

func (s *Server) lookupInstance(ctx context.Context, emp, id, token string) (*Instance, int) {
	if inst := s.cache.Get(emp, id); inst != nil {
		return inst, 0
	}
	inst, status := s.api.GetInstance(ctx, id, token)
	if status != 0 {
		return nil, status
	}
	s.cache.Put(emp, id, inst)
	return inst, 0
}

// firstCapture returns the first capture group of re applied to s, or "".
func firstCapture(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}
