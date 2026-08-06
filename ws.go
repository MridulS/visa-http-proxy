package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// serveWS handles a WebSocket upgrade: auth, optional notebook lifecycle, then a
// transparent byte relay between the client and the upstream.
func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	h := s.match(r.URL.EscapedPath())
	if h == nil || !h.WS {
		http.NotFound(w, r) // CLEAN: no ws handler -> 404 (Node hung)
		return
	}
	target, status, msg := s.resolve(r, h)
	if status != 0 {
		http.Error(w, msg, status) // CLEAN: 401 no-token / 404 bad-instance (Node hung)
		return
	}

	token, _ := ExtractToken(r) // captured for the live notebook/close

	// Jupyter notebook lifecycle (service handlers are no-ops).
	closeSession := func() {}
	if h.Type == "jupyter" {
		if k, ok := parseSession(r.URL.RequestURI()); ok {
			s.nb.OnOpen(k, token) // open failure swallowed; WS still proceeds
			closeSession = sync.OnceFunc(func() { s.nb.OnClose(k, token) })
		}
	}
	// Once the session is opened, every exit path must fire notebook/close —
	// including a panic, which would otherwise leave the session open until the
	// next restart's zombie cleanup. The relay below closes it earlier, while the
	// shutdown WaitGroup is still held; OnceFunc makes this one a no-op then.
	defer closeSession()

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket not supported", http.StatusInternalServerError)
		return
	}
	clientConn, clientBuf, err := hj.Hijack()
	if err != nil {
		return
	}
	defer clientConn.Close()
	s.trackWS(clientConn) // so graceful shutdown can close it (firing notebook/close)
	defer s.untrackWS(clientConn)

	upConn, err := net.DialTimeout("tcp", target.host, 10*time.Second)
	if err != nil {
		log.Printf("ws upstream dial failed: %v", err)
		return
	}
	defer upConn.Close()

	if err := writeUpgradeRequest(upConn, r, target); err != nil {
		log.Printf("ws upgrade write failed: %v", err)
		return
	}

	// Transparent relay: the upstream's 101 + chosen subprotocol + all frames
	// (incl. close) flow back untouched. First direction to finish ends it.
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upConn, clientBuf); done <- struct{}{} }()
	go func() { _, _ = io.Copy(clientConn, upConn); done <- struct{}{} }()
	<-done

	closeSession()
}

// writeUpgradeRequest writes the (path-rewritten) upgrade request to the upstream,
// passing the client's Upgrade/Connection/Sec-WebSocket-* headers through verbatim
// so the upstream computes Sec-WebSocket-Accept against the client's key.
func writeUpgradeRequest(w io.Writer, r *http.Request, target *rpTarget) error {
	// rawPath is already percent-encoded, so it cannot break out of the request
	// line; the check is a backstop in case a pathRewrite replacement introduces
	// a raw control character. Header values are CRLF-validated by net/http and
	// rawquery stays percent-encoded, so the request target is the only vector.
	uri := target.rawPath
	if strings.ContainsAny(uri, " \r\n") {
		return fmt.Errorf("invalid request target %q", uri)
	}
	if target.rawquery != "" {
		uri += "?" + target.rawquery
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "GET %s HTTP/1.1\r\n", uri)
	fmt.Fprintf(&b, "Host: %s\r\n", target.host)
	for name, vals := range r.Header {
		switch {
		case strings.EqualFold(name, "Host"),
			strings.EqualFold(name, "X-Forwarded-For"),
			strings.EqualFold(name, "X-Forwarded-Host"),
			strings.EqualFold(name, "X-Forwarded-Proto"):
			continue // set below from the connection, never from the client
		}
		for _, v := range vals {
			fmt.Fprintf(&b, "%s: %s\r\n", name, v)
		}
	}
	// The forwarded headers ReverseProxy.SetXForwarded writes on the HTTP path.
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		fmt.Fprintf(&b, "X-Forwarded-For: %s\r\n", ip)
	}
	fmt.Fprintf(&b, "X-Forwarded-Host: %s\r\nX-Forwarded-Proto: http\r\n", r.Host)
	b.WriteString("\r\n")
	_, err := w.Write(b.Bytes())
	return err
}
