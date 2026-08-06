package main

import (
	"bufio"
	"net/http"
	"strings"
	"testing"
)

// A request target carrying a raw control character must be rejected outright
// rather than written into the request line, where it would smuggle headers or a
// second request to the instance.
func TestWriteUpgradeRequestRejectsControlChars(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "http://proxy/", nil)
	target := &rpTarget{
		host:     "10.0.0.5:8888",
		rawPath:  "/jupyter/1/\r\nX-Injected: evil",
		rawquery: "session_id=a",
	}
	var sb strings.Builder
	if err := writeUpgradeRequest(&sb, r, target); err == nil {
		t.Fatalf("expected rejection, wrote:\n%q", sb.String())
	}
}

// The path is forwarded exactly as it arrived on the wire: percent-encoding is
// neither added nor removed, so %2e%2e cannot become a traversal segment.
func TestWriteUpgradeRequestForwardsWirePath(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "http://proxy/", nil)
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	target := &rpTarget{host: "10.0.0.5:8888", rawPath: "/socket.io/%2e%2e/foo", rawquery: "EIO=4"}
	var sb strings.Builder
	if err := writeUpgradeRequest(&sb, r, target); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if !strings.HasPrefix(out, "GET /socket.io/%2e%2e/foo?EIO=4 HTTP/1.1\r\n") {
		t.Fatalf("unexpected request line:\n%q", out)
	}
	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(out)))
	if err != nil {
		t.Fatalf("upstream request is not well-formed: %v\n%q", err, out)
	}
	if got := req.Header.Get("Sec-Websocket-Key"); got != "dGhlIHNhbXBsZSBub25jZQ==" {
		t.Fatalf("Sec-WebSocket-Key not forwarded, got %q", got)
	}
}

// A client-supplied X-Forwarded-* must never reach the instance: the WS path
// sets the same forwarded headers ReverseProxy.SetXForwarded sets on HTTP.
func TestWriteUpgradeRequestForwardedHeaders(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "http://proxy/", nil)
	r.Host = "visa.example:443"
	r.RemoteAddr = "10.1.2.3:5555"
	r.Header.Set("X-Forwarded-For", "9.9.9.9")
	r.Header.Set("X-Forwarded-Proto", "https")
	var sb strings.Builder
	if err := writeUpgradeRequest(&sb, r, &rpTarget{host: "10.0.0.5:8888", rawPath: "/jupyter/1/x"}); err != nil {
		t.Fatal(err)
	}
	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(sb.String())))
	if err != nil {
		t.Fatal(err)
	}
	for header, want := range map[string]string{
		"X-Forwarded-For":   "10.1.2.3",
		"X-Forwarded-Host":  "visa.example:443",
		"X-Forwarded-Proto": "http",
	} {
		if got := req.Header.Values(header); len(got) != 1 || got[0] != want {
			t.Errorf("%s = %q, want [%q]", header, got, want)
		}
	}
}
