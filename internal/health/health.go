// Package health serves HTTP liveness (/healthz) and readiness (/readyz) probes.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// Server exposes /healthz and /readyz HTTP endpoints with explicit timeouts.
type Server struct {
	ready  atomic.Bool
	server *http.Server
	addr   string
	errCh  chan error
}

// New creates a new unstarted health Server in the not-ready state.
func New() *Server {
	return &Server{
		errCh: make(chan error, 1),
	}
}

// SetReady updates the readiness state reported by /readyz.
func (s *Server) SetReady(ready bool) {
	s.ready.Store(ready)
}

// Ready reports the current readiness state.
func (s *Server) Ready() bool {
	return s.ready.Load()
}

// Addr returns the bound network address after Start or StartAddr succeeds.
func (s *Server) Addr() string {
	return s.addr
}

// Errors returns a channel that receives fatal server errors.
func (s *Server) Errors() <-chan error {
	return s.errCh
}

// Handler returns the HTTP handler serving /healthz and /readyz.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	return mux
}

// Start binds to ":"+port and begins serving health endpoints.
func (s *Server) Start(port string) error {
	return s.StartAddr(":" + port)
}

// StartAddr binds to addr (for example "127.0.0.1:0") and begins serving.
func (s *Server) StartAddr(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.addr = listener.Addr().String()
	s.server = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.errCh <- err
		}
		close(s.errCh)
	}()

	return nil
}

// Shutdown marks the server not ready and gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.SetReady(false)
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "not_ready"})
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}
