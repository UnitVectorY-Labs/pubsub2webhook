package forwarder

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/pem"
	"hash"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/UnitVectorY-Labs/pubsub2webhook/internal/config"
)

func baseConfig(url string) *config.Config {
	return &config.Config{
		PubSubSubscription:           "projects/p/subscriptions/s",
		WebhookURL:                   url,
		WebhookMethod:                "POST",
		WebhookContentType:           "application/json",
		WebhookTimeout:               5 * time.Second,
		WebhookHMACHeader:            "X-Hub-Signature-256",
		WebhookHMACAlgorithm:         "sha256",
		WebhookHMACPrefix:            "sha256=",
		WebhookAckOn4xx:              true,
		PayloadCompressionAttribute:  "compression",
		MaxPayloadBytes:              config.DefaultMaxPayloadBytes,
		PubSubMaxOutstandingMessages: 10,
		PubSubMaxOutstandingBytes:    10485760,
		PubSubNumGoroutines:          1,
		HealthPort:                   "8080",
		LogLevel:                     "info",
	}
}

func TestForwardHeadersMethodAndBody(t *testing.T) {
	var gotMethod string
	var gotHeaders http.Header
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeaders = r.Header.Clone()
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := baseConfig(srv.URL)
	cfg.WebhookMethod = "PUT"
	cfg.WebhookStaticHeaders = map[string]string{
		"X-Static":     "static-val",
		"Content-Type": "application/vnd.custom+json",
	}
	cfg.WebhookAttributeHeaders = map[string]string{
		"gh_event":    "X-GitHub-Event",
		"gh_delivery": "X-GitHub-Delivery",
		"missing":     "X-Should-Not-Appear",
	}
	cfg.WebhookIncludePubSubHeaders = true

	fwd, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	pubTime := time.Date(2026, 11, 21, 8, 30, 0, 0, time.UTC)
	payload := []byte(`{"event":"push"}`)
	res, err := fwd.Forward(context.Background(), Message{
		ID:          "msg-123",
		PublishTime: pubTime,
		Data:        payload,
		Attributes: map[string]string{
			"gh_event":    "push",
			"gh_delivery": "abc-def",
			"missing":     "",
		},
	})
	if err != nil {
		t.Fatalf("Forward() error: %v", err)
	}
	if !res.Ack || res.StatusCode != http.StatusAccepted || res.Acked4xx {
		t.Fatalf("unexpected result: %+v", res)
	}
	if gotMethod != "PUT" {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if string(gotBody) != string(payload) {
		t.Errorf("body = %q, want %q", gotBody, payload)
	}
	if gotHeaders.Get("Content-Type") != "application/vnd.custom+json" {
		t.Errorf("Content-Type = %q", gotHeaders.Get("Content-Type"))
	}
	if gotHeaders.Get("X-Static") != "static-val" {
		t.Errorf("X-Static = %q", gotHeaders.Get("X-Static"))
	}
	if gotHeaders.Get("X-GitHub-Event") != "push" {
		t.Errorf("X-GitHub-Event = %q", gotHeaders.Get("X-GitHub-Event"))
	}
	if gotHeaders.Get("X-GitHub-Delivery") != "abc-def" {
		t.Errorf("X-GitHub-Delivery = %q", gotHeaders.Get("X-GitHub-Delivery"))
	}
	if gotHeaders.Get("X-Should-Not-Appear") != "" {
		t.Errorf("expected X-Should-Not-Appear to be omitted, got %q", gotHeaders.Get("X-Should-Not-Appear"))
	}
	if gotHeaders.Get("X-PubSub-Message-ID") != "msg-123" {
		t.Errorf("X-PubSub-Message-ID = %q", gotHeaders.Get("X-PubSub-Message-ID"))
	}
	if gotHeaders.Get("X-PubSub-Publish-Time") != "2026-11-21T08:30:00Z" {
		t.Errorf("X-PubSub-Publish-Time = %q", gotHeaders.Get("X-PubSub-Publish-Time"))
	}
}

func TestForwardHMACSignatures(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	secret := []byte("top-secret-key")

	tests := []struct {
		name   string
		algo   string
		header string
		prefix string
		hashFn func() hash.Hash
	}{
		{name: "sha256 default", algo: "sha256", header: "X-Hub-Signature-256", prefix: "sha256=", hashFn: sha256.New},
		{name: "sha1", algo: "sha1", header: "X-Hub-Signature", prefix: "sha1=", hashFn: sha1.New},
		{name: "sha512 raw hex", algo: "sha512", header: "X-Signature-512", prefix: "", hashFn: sha512.New},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotSig string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotSig = r.Header.Get(tc.header)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			cfg := baseConfig(srv.URL)
			cfg.WebhookHMACAlgorithm = tc.algo
			cfg.WebhookHMACHeader = tc.header
			cfg.WebhookHMACPrefix = tc.prefix
			cfg.ResolvedHMACSecret = secret

			fwd, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}

			res, err := fwd.Forward(context.Background(), Message{ID: "1", Data: body})
			if err != nil || !res.Ack {
				t.Fatalf("Forward() res=%+v err=%v", res, err)
			}

			mac := hmac.New(tc.hashFn, secret)
			mac.Write(body)
			want := tc.prefix + hex.EncodeToString(mac.Sum(nil))
			if gotSig != want {
				t.Errorf("signature = %q, want %q", gotSig, want)
			}
		})
	}
}

func TestForwardStatusAckNackSemantics(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		ackOn4xx     bool
		wantAck      bool
		wantAcked4xx bool
		wantErr      bool
	}{
		{name: "200 OK", status: http.StatusOK, ackOn4xx: true, wantAck: true},
		{name: "204 No Content", status: http.StatusNoContent, ackOn4xx: false, wantAck: true},
		{name: "400 Ack when AckOn4xx=true", status: http.StatusBadRequest, ackOn4xx: true, wantAck: true, wantAcked4xx: true},
		{name: "404 Ack when AckOn4xx=true", status: http.StatusNotFound, ackOn4xx: true, wantAck: true, wantAcked4xx: true},
		{name: "400 Nack when AckOn4xx=false", status: http.StatusBadRequest, ackOn4xx: false, wantAck: false, wantErr: true},
		{name: "404 Nack when AckOn4xx=false", status: http.StatusNotFound, ackOn4xx: false, wantAck: false, wantErr: true},
		{name: "408 always Nack", status: http.StatusRequestTimeout, ackOn4xx: true, wantAck: false, wantErr: true},
		{name: "429 always Nack", status: http.StatusTooManyRequests, ackOn4xx: true, wantAck: false, wantErr: true},
		{name: "500 always Nack", status: http.StatusInternalServerError, ackOn4xx: true, wantAck: false, wantErr: true},
		{name: "503 always Nack", status: http.StatusServiceUnavailable, ackOn4xx: true, wantAck: false, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("response body"))
			}))
			defer srv.Close()

			cfg := baseConfig(srv.URL)
			cfg.WebhookAckOn4xx = tc.ackOn4xx
			fwd, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}

			res, err := fwd.Forward(context.Background(), Message{ID: "1", Data: []byte(`{}`)})
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if res.Ack != tc.wantAck {
				t.Errorf("Ack = %v, want %v", res.Ack, tc.wantAck)
			}
			if res.Acked4xx != tc.wantAcked4xx {
				t.Errorf("Acked4xx = %v, want %v", res.Acked4xx, tc.wantAcked4xx)
			}
			if res.StatusCode != tc.status {
				t.Errorf("StatusCode = %d, want %d", res.StatusCode, tc.status)
			}
		})
	}
}

func TestForwardTimeoutNacks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(300 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	cfg := baseConfig(srv.URL)
	cfg.WebhookTimeout = 40 * time.Millisecond
	fwd, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	res, err := fwd.Forward(context.Background(), Message{ID: "1", Data: []byte(`{}`)})
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if res.Ack {
		t.Errorf("expected Ack=false on timeout")
	}
}

func TestForwardCustomCAFile(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Without CA file, TLS verification must fail.
	cfgNoCA := baseConfig(srv.URL)
	fwdNoCA, err := New(cfgNoCA)
	if err != nil {
		t.Fatalf("New() without CA error: %v", err)
	}
	if res, err := fwdNoCA.Forward(context.Background(), Message{ID: "1", Data: []byte(`{}`)}); err == nil || res.Ack {
		t.Fatalf("expected TLS failure without CA, got res=%+v err=%v", res, err)
	}

	// Write the test server's certificate to a PEM file and configure WEBHOOK_CA_FILE.
	caPath := filepath.Join(t.TempDir(), "server-ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: srv.Certificate().Raw,
	})
	if err := os.WriteFile(caPath, pemBytes, 0o600); err != nil {
		t.Fatalf("write CA file: %v", err)
	}

	cfgWithCA := baseConfig(srv.URL)
	cfgWithCA.WebhookCAFile = caPath
	fwdWithCA, err := New(cfgWithCA)
	if err != nil {
		t.Fatalf("New() with CA error: %v", err)
	}

	res, err := fwdWithCA.Forward(context.Background(), Message{ID: "1", Data: []byte(`{}`)})
	if err != nil || !res.Ack || res.StatusCode != http.StatusOK {
		t.Fatalf("expected TLS success with custom CA, got res=%+v err=%v", res, err)
	}
}
