package health

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestHealthServerEndpointsAndTransitions(t *testing.T) {
	srv := New()
	if err := srv.StartAddr("127.0.0.1:0"); err != nil {
		t.Fatalf("StartAddr() error: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	baseURL := "http://" + srv.Addr()
	client := &http.Client{Timeout: 2 * time.Second}

	// /healthz always returns 200
	resp, err := client.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status = %d, want 200", resp.StatusCode)
	}

	// /readyz starts as 503
	resp, err = client.Get(baseURL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz (initial): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("/readyz initial status = %d, want 503", resp.StatusCode)
	}

	// Transition to ready -> 200
	srv.SetReady(true)
	if !srv.Ready() {
		t.Fatalf("Ready() = false, want true")
	}
	resp, err = client.Get(baseURL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz (ready): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/readyz ready status = %d, want 200", resp.StatusCode)
	}

	// Transition back to not ready -> 503
	srv.SetReady(false)
	resp, err = client.Get(baseURL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz (not ready): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("/readyz after SetReady(false) status = %d, want 503", resp.StatusCode)
	}
}
