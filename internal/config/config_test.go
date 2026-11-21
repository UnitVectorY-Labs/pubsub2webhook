package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mapEnv(m map[string]string) LookupEnv {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func writeTestCert(t *testing.T, dir string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	p := filepath.Join(dir, "ca.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	return p
}

func TestParseAndValidateDefaults(t *testing.T) {
	env := mapEnv(map[string]string{
		"PUBSUB_SUBSCRIPTION": "projects/my-proj/subscriptions/my-sub",
		"WEBHOOK_URL":         "https://example.com/webhook",
	})

	cfg, meta, err := Parse(nil, env)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if meta.ShowVersion || meta.CheckConfig {
		t.Fatalf("unexpected meta flags: %+v", meta)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}

	if cfg.WebhookMethod != "POST" {
		t.Errorf("WebhookMethod = %q, want POST", cfg.WebhookMethod)
	}
	if cfg.WebhookContentType != "application/json" {
		t.Errorf("WebhookContentType = %q, want application/json", cfg.WebhookContentType)
	}
	if cfg.WebhookTimeout != 10*time.Second {
		t.Errorf("WebhookTimeout = %s, want 10s", cfg.WebhookTimeout)
	}
	if cfg.WebhookIncludePubSubHeaders {
		t.Errorf("WebhookIncludePubSubHeaders = true, want false")
	}
	if cfg.WebhookHMACHeader != "X-Hub-Signature-256" {
		t.Errorf("WebhookHMACHeader = %q, want X-Hub-Signature-256", cfg.WebhookHMACHeader)
	}
	if cfg.WebhookHMACAlgorithm != "sha256" {
		t.Errorf("WebhookHMACAlgorithm = %q, want sha256", cfg.WebhookHMACAlgorithm)
	}
	if cfg.WebhookHMACPrefix != "sha256=" {
		t.Errorf("WebhookHMACPrefix = %q, want sha256=", cfg.WebhookHMACPrefix)
	}
	if !cfg.WebhookAckOn4xx {
		t.Errorf("WebhookAckOn4xx = false, want true")
	}
	if cfg.PayloadCompressionAttribute != "compression" {
		t.Errorf("PayloadCompressionAttribute = %q, want compression", cfg.PayloadCompressionAttribute)
	}
	if cfg.MaxPayloadBytes != 26214400 {
		t.Errorf("MaxPayloadBytes = %d, want 26214400", cfg.MaxPayloadBytes)
	}
	if cfg.PubSubMaxOutstandingMessages != 10 {
		t.Errorf("PubSubMaxOutstandingMessages = %d, want 10", cfg.PubSubMaxOutstandingMessages)
	}
	if cfg.PubSubMaxOutstandingBytes != 10485760 {
		t.Errorf("PubSubMaxOutstandingBytes = %d, want 10485760", cfg.PubSubMaxOutstandingBytes)
	}
	if cfg.PubSubNumGoroutines != 1 {
		t.Errorf("PubSubNumGoroutines = %d, want 1", cfg.PubSubNumGoroutines)
	}
	if cfg.HealthPort != "8080" || !cfg.HealthEnabled() {
		t.Errorf("HealthPort = %q, HealthEnabled = %v", cfg.HealthPort, cfg.HealthEnabled())
	}
	if cfg.LogLevel != "info" || cfg.SlogLevel() != slog.LevelInfo {
		t.Errorf("LogLevel = %q, SlogLevel = %v", cfg.LogLevel, cfg.SlogLevel())
	}
}

func TestParseFlagOverridesEnv(t *testing.T) {
	env := mapEnv(map[string]string{
		"PUBSUB_SUBSCRIPTION":          "projects/env-proj/subscriptions/env-sub",
		"WEBHOOK_URL":                  "http://env.local/hook",
		"WEBHOOK_METHOD":               "PUT",
		"WEBHOOK_TIMEOUT":              "30s",
		"WEBHOOK_ACK_ON_4XX":           "true",
		"WEBHOOK_HMAC_ALGORITHM":       "sha512",
		"PAYLOAD_COMPRESSION_ATTRIBUTE": "compression",
		"HEALTH_PORT":                  "8080",
		"LOG_LEVEL":                    "warn",
	})

	args := []string{
		"--pubsub-subscription", "projects/flag-proj/subscriptions/flag-sub",
		"--webhook-url", "https://flag.example/api/webhook",
		"--webhook-method", "PATCH",
		"--webhook-timeout", "5s",
		"--webhook-ack-on-4xx=false",
		"--webhook-include-pubsub-headers=true",
		"--webhook-hmac-prefix=",
		"--payload-compression-attribute=",
		"--health-port=0",
		"--log-level=debug",
		"--check-config",
	}

	cfg, meta, err := Parse(args, env)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if !meta.CheckConfig {
		t.Fatalf("expected CheckConfig = true")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}

	if cfg.PubSubSubscription != "projects/flag-proj/subscriptions/flag-sub" {
		t.Errorf("PubSubSubscription = %q", cfg.PubSubSubscription)
	}
	if cfg.WebhookURL != "https://flag.example/api/webhook" {
		t.Errorf("WebhookURL = %q", cfg.WebhookURL)
	}
	if cfg.WebhookMethod != "PATCH" {
		t.Errorf("WebhookMethod = %q", cfg.WebhookMethod)
	}
	if cfg.WebhookTimeout != 5*time.Second {
		t.Errorf("WebhookTimeout = %s", cfg.WebhookTimeout)
	}
	if cfg.WebhookAckOn4xx {
		t.Errorf("WebhookAckOn4xx = true, want false")
	}
	if !cfg.WebhookIncludePubSubHeaders {
		t.Errorf("WebhookIncludePubSubHeaders = false, want true")
	}
	if cfg.WebhookHMACPrefix != "" {
		t.Errorf("WebhookHMACPrefix = %q, want empty", cfg.WebhookHMACPrefix)
	}
	if cfg.PayloadCompressionAttribute != "" {
		t.Errorf("PayloadCompressionAttribute = %q, want empty", cfg.PayloadCompressionAttribute)
	}
	if cfg.HealthEnabled() {
		t.Errorf("HealthEnabled = true, want false")
	}
	if cfg.SlogLevel() != slog.LevelDebug {
		t.Errorf("SlogLevel = %v, want debug", cfg.SlogLevel())
	}
}

func TestEnvEmptyDisablesCompressionAndHealthAndHMACPrefix(t *testing.T) {
	env := mapEnv(map[string]string{
		"PUBSUB_SUBSCRIPTION":           "projects/p/subscriptions/s",
		"WEBHOOK_URL":                   "https://example.com/hook",
		"PAYLOAD_COMPRESSION_ATTRIBUTE": "",
		"HEALTH_PORT":                   "",
		"WEBHOOK_HMAC_ALGORITHM":        "sha512",
		"WEBHOOK_HMAC_PREFIX":           "",
	})

	cfg, _, err := Parse(nil, env)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if cfg.PayloadCompressionAttribute != "" {
		t.Errorf("PayloadCompressionAttribute = %q, want empty", cfg.PayloadCompressionAttribute)
	}
	if cfg.HealthEnabled() {
		t.Errorf("HealthEnabled = true, want false")
	}
	if cfg.WebhookHMACPrefix != "" {
		t.Errorf("WebhookHMACPrefix = %q, want empty", cfg.WebhookHMACPrefix)
	}
}

func TestHMACPrefixDefaultsToAlgorithm(t *testing.T) {
	for _, algo := range []string{"sha256", "sha1", "sha512"} {
		t.Run(algo, func(t *testing.T) {
			env := mapEnv(map[string]string{
				"PUBSUB_SUBSCRIPTION":    "projects/p/subscriptions/s",
				"WEBHOOK_URL":            "https://example.com/hook",
				"WEBHOOK_HMAC_ALGORITHM": algo,
			})
			cfg, _, err := Parse(nil, env)
			if err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() error: %v", err)
			}
			want := algo + "="
			if cfg.WebhookHMACPrefix != want {
				t.Errorf("WebhookHMACPrefix = %q, want %q", cfg.WebhookHMACPrefix, want)
			}
		})
	}
}

func TestHMACSecretFileAndMutualExclusion(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "hmac-secret")
	if err := os.WriteFile(secretPath, []byte("super-secret-value\r\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	env := mapEnv(map[string]string{
		"PUBSUB_SUBSCRIPTION":     "projects/p/subscriptions/s",
		"WEBHOOK_URL":             "https://example.com/hook",
		"WEBHOOK_HMAC_SECRET_FILE": secretPath,
	})
	cfg, _, err := Parse(nil, env)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if string(cfg.ResolvedHMACSecret) != "super-secret-value" {
		t.Errorf("ResolvedHMACSecret = %q, want super-secret-value", string(cfg.ResolvedHMACSecret))
	}

	// Mutually exclusive check
	bothEnv := mapEnv(map[string]string{
		"PUBSUB_SUBSCRIPTION":      "projects/p/subscriptions/s",
		"WEBHOOK_URL":              "https://example.com/hook",
		"WEBHOOK_HMAC_SECRET":      "inline-secret",
		"WEBHOOK_HMAC_SECRET_FILE": secretPath,
	})
	cfg2, _, err := Parse(nil, bothEnv)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if err := cfg2.Validate(); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got %v", err)
	}

	// Empty secret file
	emptyPath := filepath.Join(dir, "empty-secret")
	if err := os.WriteFile(emptyPath, []byte("\n"), 0o600); err != nil {
		t.Fatalf("write empty secret file: %v", err)
	}
	emptyEnv := mapEnv(map[string]string{
		"PUBSUB_SUBSCRIPTION":      "projects/p/subscriptions/s",
		"WEBHOOK_URL":              "https://example.com/hook",
		"WEBHOOK_HMAC_SECRET_FILE": emptyPath,
	})
	cfg3, _, err := Parse(nil, emptyEnv)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if err := cfg3.Validate(); err == nil {
		t.Fatalf("expected error for empty secret file")
	}
}

func TestCAFileValidation(t *testing.T) {
	dir := t.TempDir()
	validCA := writeTestCert(t, dir)
	badCA := filepath.Join(dir, "bad.pem")
	if err := os.WriteFile(badCA, []byte("not a cert"), 0o600); err != nil {
		t.Fatalf("write bad ca: %v", err)
	}

	validEnv := mapEnv(map[string]string{
		"PUBSUB_SUBSCRIPTION": "projects/p/subscriptions/s",
		"WEBHOOK_URL":         "https://example.com/hook",
		"WEBHOOK_CA_FILE":     validCA,
	})
	cfg, _, err := Parse(nil, validEnv)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error for valid CA: %v", err)
	}

	badEnv := mapEnv(map[string]string{
		"PUBSUB_SUBSCRIPTION": "projects/p/subscriptions/s",
		"WEBHOOK_URL":         "https://example.com/hook",
		"WEBHOOK_CA_FILE":     badCA,
	})
	cfg2, _, err := Parse(nil, badEnv)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if err := cfg2.Validate(); err == nil {
		t.Fatalf("expected error for invalid PEM CA file")
	}
}

func TestParseHeaderMap(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "empty",
			input: "   ",
			want:  nil,
		},
		{
			name:  "key-value pairs",
			input: "gh_event=X-GitHub-Event, gh_delivery=X-GitHub-Delivery",
			want: map[string]string{
				"gh_event":    "X-GitHub-Event",
				"gh_delivery": "X-GitHub-Delivery",
			},
		},
		{
			name:  "value containing equals",
			input: "Authorization=Basic dXNlcjpwYXNz==,X-Custom=a=b",
			want: map[string]string{
				"Authorization": "Basic dXNlcjpwYXNz==",
				"X-Custom":      "a=b",
			},
		},
		{
			name:  "json object",
			input: `{"gh_event":"X-GitHub-Event","X-List":"a,b"}`,
			want: map[string]string{
				"gh_event": "X-GitHub-Event",
				"X-List":   "a,b",
			},
		},
		{
			name:    "invalid json",
			input:   `{"gh_event": 123}`,
			wantErr: true,
		},
		{
			name:    "missing equals",
			input:   "gh_event",
			wantErr: true,
		},
		{
			name:    "empty key",
			input:   "=X-Header",
			wantErr: true,
		},
		{
			name:    "trailing comma",
			input:   "a=b,",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseHeaderMap(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len(got) = %d, want %d (%v vs %v)", len(got), len(tc.want), got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("got[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestParseSubscription(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		project string
		sub     string
		wantErr bool
	}{
		{name: "valid", input: "projects/my-proj/subscriptions/my-sub", project: "my-proj", sub: "my-sub"},
		{name: "empty", input: "", wantErr: true},
		{name: "short", input: "projects/my-proj/subscriptions", wantErr: true},
		{name: "wrong prefix", input: "topics/my-proj/subscriptions/my-sub", wantErr: true},
		{name: "missing project", input: "projects//subscriptions/my-sub", wantErr: true},
		{name: "missing sub", input: "projects/my-proj/subscriptions/", wantErr: true},
		{name: "extra segment", input: "projects/p/subscriptions/s/extra", wantErr: true},
		{name: "whitespace", input: "projects/my proj/subscriptions/my-sub", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, s, err := ParseSubscription(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got (%q, %q)", tc.input, p, s)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p != tc.project || s != tc.sub {
				t.Errorf("got (%q, %q), want (%q, %q)", p, s, tc.project, tc.sub)
			}
		})
	}
}

func TestValidateErrors(t *testing.T) {
	base := func() map[string]string {
		return map[string]string{
			"PUBSUB_SUBSCRIPTION": "projects/p/subscriptions/s",
			"WEBHOOK_URL":         "https://example.com/hook",
		}
	}

	cases := []struct {
		name     string
		override map[string]string
		wantSub  string
	}{
		{name: "missing subscription", override: map[string]string{"PUBSUB_SUBSCRIPTION": ""}, wantSub: "PUBSUB_SUBSCRIPTION"},
		{name: "missing url", override: map[string]string{"WEBHOOK_URL": ""}, wantSub: "WEBHOOK_URL"},
		{name: "bad url scheme", override: map[string]string{"WEBHOOK_URL": "ftp://example.com/hook"}, wantSub: "scheme"},
		{name: "missing url host", override: map[string]string{"WEBHOOK_URL": "https:///hook"}, wantSub: "host"},
		{name: "lowercase method", override: map[string]string{"WEBHOOK_METHOD": "post"}, wantSub: "WEBHOOK_METHOD"},
		{name: "zero timeout", override: map[string]string{"WEBHOOK_TIMEOUT": "0s"}, wantSub: "WEBHOOK_TIMEOUT"},
		{name: "negative timeout", override: map[string]string{"WEBHOOK_TIMEOUT": "-1s"}, wantSub: "WEBHOOK_TIMEOUT"},
		{name: "bad hmac algorithm", override: map[string]string{"WEBHOOK_HMAC_ALGORITHM": "md5"}, wantSub: "WEBHOOK_HMAC_ALGORITHM"},
		{name: "bad hmac header", override: map[string]string{"WEBHOOK_HMAC_HEADER": "Bad Header"}, wantSub: "WEBHOOK_HMAC_HEADER"},
		{name: "bad attr header name", override: map[string]string{"WEBHOOK_ATTRIBUTE_HEADERS": "gh_event=Bad Header"}, wantSub: "WEBHOOK_ATTRIBUTE_HEADERS"},
		{name: "bad static header name", override: map[string]string{"WEBHOOK_STATIC_HEADERS": "Bad Header=val"}, wantSub: "WEBHOOK_STATIC_HEADERS"},
		{name: "zero max payload", override: map[string]string{"MAX_PAYLOAD_BYTES": "0"}, wantSub: "MAX_PAYLOAD_BYTES"},
		{name: "zero max outstanding messages", override: map[string]string{"PUBSUB_MAX_OUTSTANDING_MESSAGES": "0"}, wantSub: "PUBSUB_MAX_OUTSTANDING_MESSAGES"},
		{name: "zero max outstanding bytes", override: map[string]string{"PUBSUB_MAX_OUTSTANDING_BYTES": "0"}, wantSub: "PUBSUB_MAX_OUTSTANDING_BYTES"},
		{name: "zero num goroutines", override: map[string]string{"PUBSUB_NUM_GOROUTINES": "0"}, wantSub: "PUBSUB_NUM_GOROUTINES"},
		{name: "bad health port", override: map[string]string{"HEALTH_PORT": "99999"}, wantSub: "HEALTH_PORT"},
		{name: "bad log level", override: map[string]string{"LOG_LEVEL": "trace"}, wantSub: "LOG_LEVEL"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := base()
			for k, v := range tc.override {
				env[k] = v
			}
			cfg, _, err := Parse(nil, mapEnv(env))
			if err != nil {
				t.Fatalf("Parse() unexpected error: %v", err)
			}
			err = cfg.Validate()
			if err == nil {
				t.Fatalf("Validate() expected error containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("Validate() error = %q, want substring %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestParseEnvTypeErrorsAndVersionFlag(t *testing.T) {
	_, meta, err := Parse([]string{"--version"}, mapEnv(nil))
	if err != nil || !meta.ShowVersion {
		t.Fatalf("expected ShowVersion=true, err=nil; got meta=%+v err=%v", meta, err)
	}

	_, _, err = Parse(nil, mapEnv(map[string]string{"WEBHOOK_TIMEOUT": "not-a-duration"}))
	if err == nil {
		t.Fatalf("expected error for invalid WEBHOOK_TIMEOUT env")
	}

	// Valid CLI override should mask invalid env value
	cfg, _, err := Parse([]string{"--webhook-timeout", "3s"}, mapEnv(map[string]string{
		"PUBSUB_SUBSCRIPTION": "projects/p/subscriptions/s",
		"WEBHOOK_URL":         "https://example.com",
		"WEBHOOK_TIMEOUT":     "not-a-duration",
	}))
	if err != nil {
		t.Fatalf("expected flag override to succeed, got %v", err)
	}
	if cfg.WebhookTimeout != 3*time.Second {
		t.Errorf("WebhookTimeout = %s, want 3s", cfg.WebhookTimeout)
	}
}
