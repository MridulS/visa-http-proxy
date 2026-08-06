// Command visa-http-proxy is a minimal, stdlib-only reverse proxy that
// authenticates requests, resolves the target VISA instance from the URL and the
// VISA API, optionally rewrites the path, and forwards HTTP and WebSocket traffic.
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags)

	cfg := LoadConfig()
	handlers, err := LoadHandlers("proxy.conf.json")
	if err != nil {
		log.Fatalf("failed to load proxy.conf.json: %v", err)
	}
	for i := range handlers {
		log.Printf("Adding proxy '%s' for paths matching '%s'", handlers[i].Name, handlers[i].Match)
	}
	log.Printf("Using VISA api server at %s", cfg.APIBase)

	store := NewStore(cfg.StorageDir)
	api := NewAPIClient(cfg.APIBase)
	cache := NewCache(cfg.CacheTTL)
	cache.StartSweeper(cfg.CacheTTL) // reclaim expired entries (keys churn with token rotation)
	nb := NewNotebook(api, store)

	// Close any sessions persisted by a previous run before accepting traffic.
	nb.CleanupZombies()

	srv := NewServer(handlers, api, cache, nb)
	httpServer := &http.Server{
		Addr:    net.JoinHostPort(cfg.ServerHost, strconv.Itoa(cfg.ServerPort)),
		Handler: srv,
		// Bound the header read and idle keep-alive (slowloris / resource guards).
		// ReadTimeout/WriteTimeout are intentionally unset so large bodies and
		// long-lived WebSocket/streaming connections are not cut off.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	signalled, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	idleClosed := make(chan struct{})
	go func() {
		defer close(idleClosed)
		<-signalled.Done()
		log.Printf("shutdown signal received")
		// Tear down live WebSocket relays first so each fires notebook/close
		// (hijacked conns are invisible to http.Server.Shutdown), then drain
		// in-flight HTTP requests.
		srv.ShutdownWebSockets(5 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			httpServer.Close()
		}
	}()

	log.Printf("Running proxy server at %s", httpServer.Addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
	<-idleClosed
}
