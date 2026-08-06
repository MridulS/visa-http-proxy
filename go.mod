module github.com/MridulS/visa-http-proxy

go 1.26

// Minimum toolchain, not just minimum language version: `go 1.26` alone selects
// go1.26.0, which govulncheck reports as carrying 13 reachable stdlib
// vulnerabilities — among them a ReverseProxy query-forwarding bug and an
// HTTP/2 infinite loop, both squarely on this proxy's path. Raise this floor
// when a later patch release fixes something reachable; a newer local toolchain
// is still used as-is.
toolchain go1.26.5
