// Package forwarder replays Pub/Sub messages as HTTP webhook requests.
package forwarder

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/UnitVectorY-Labs/pubsub2webhook/internal/config"
)

const (
	maxDrainBytes   = 64 << 10 // 64 KiB for connection reuse
	maxSnippetBytes = 256
)

// Message represents a decompressed Pub/Sub message ready for HTTP forwarding.
type Message struct {
	ID          string
	PublishTime time.Time
	Data        []byte
	Attributes  map[string]string
}

// Result describes the outcome of forwarding a single webhook request.
type Result struct {
	StatusCode      int
	Ack             bool
	Acked4xx        bool
	ResponseSnippet string
}

// Forwarder sends Pub/Sub messages to a configured HTTP webhook endpoint.
type Forwarder struct {
	client             *http.Client
	url                string
	method             string
	contentType        string
	staticHeaders      map[string]string
	attributeHeaders   map[string]string
	includePubSubHdrs  bool
	ackOn4xx           bool
	hmacSecret         []byte
	hmacHeader         string
	hmacPrefix         string
	hmacHashConstructor func() hash.Hash
}

// New constructs a Forwarder from a validated Config.
func New(cfg *config.Config) (*Forwarder, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	if cfg.WebhookCAFile != "" {
		pemData, err := os.ReadFile(cfg.WebhookCAFile)
		if err != nil {
			return nil, fmt.Errorf("reading CA file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("CA file %q contains no valid PEM certificates", cfg.WebhookCAFile)
		}
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		}
		transport.TLSClientConfig.RootCAs = pool
	}

	var hashFn func() hash.Hash
	switch cfg.WebhookHMACAlgorithm {
	case "sha256":
		hashFn = sha256.New
	case "sha1":
		hashFn = sha1.New
	case "sha512":
		hashFn = sha512.New
	default:
		return nil, fmt.Errorf("unsupported HMAC algorithm %q", cfg.WebhookHMACAlgorithm)
	}

	var secretCopy []byte
	if len(cfg.ResolvedHMACSecret) > 0 {
		secretCopy = append([]byte(nil), cfg.ResolvedHMACSecret...)
	}

	staticCopy := make(map[string]string, len(cfg.WebhookStaticHeaders))
	for k, v := range cfg.WebhookStaticHeaders {
		staticCopy[k] = v
	}
	attrCopy := make(map[string]string, len(cfg.WebhookAttributeHeaders))
	for k, v := range cfg.WebhookAttributeHeaders {
		attrCopy[k] = v
	}

	return &Forwarder{
		client: &http.Client{
			Timeout:   cfg.WebhookTimeout,
			Transport: transport,
		},
		url:                 cfg.WebhookURL,
		method:              cfg.WebhookMethod,
		contentType:         cfg.WebhookContentType,
		staticHeaders:       staticCopy,
		attributeHeaders:    attrCopy,
		includePubSubHdrs:   cfg.WebhookIncludePubSubHeaders,
		ackOn4xx:            cfg.WebhookAckOn4xx,
		hmacSecret:          secretCopy,
		hmacHeader:          cfg.WebhookHMACHeader,
		hmacPrefix:          cfg.WebhookHMACPrefix,
		hmacHashConstructor: hashFn,
	}, nil
}

// Forward sends msg as an HTTP request and determines whether the message
// should be acknowledged or retried.
func (f *Forwarder) Forward(ctx context.Context, msg Message) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, f.method, f.url, bytes.NewReader(msg.Data))
	if err != nil {
		return Result{Ack: false}, fmt.Errorf("building webhook request: %w", err)
	}
	req.ContentLength = int64(len(msg.Data))

	// 1. Default Content-Type
	if f.contentType != "" {
		req.Header.Set("Content-Type", f.contentType)
	}

	// 2. Static headers (can override default Content-Type)
	for name, value := range f.staticHeaders {
		req.Header.Set(name, value)
	}

	// 3. Pub/Sub attribute-mapped headers (only when attribute is present and non-empty)
	if msg.Attributes != nil {
		for attrName, headerName := range f.attributeHeaders {
			if val := msg.Attributes[attrName]; val != "" {
				if strings.ContainsAny(val, "\r\n\x00") {
					continue
				}
				req.Header.Set(headerName, val)
			}
		}
	}

	// 4. Optional Pub/Sub metadata headers
	if f.includePubSubHdrs {
		if msg.ID != "" {
			req.Header.Set("X-PubSub-Message-ID", msg.ID)
		}
		if !msg.PublishTime.IsZero() {
			req.Header.Set("X-PubSub-Publish-Time", msg.PublishTime.UTC().Format(time.RFC3339))
		}
	}

	// 5. Optional HMAC signature over outgoing body (applied last so it cannot be overridden)
	if len(f.hmacSecret) > 0 {
		req.Header.Set(f.hmacHeader, f.sign(msg.Data))
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return Result{Ack: false}, fmt.Errorf("sending webhook request: %w", err)
	}

	snippet := readSnippetAndDrain(resp.Body)
	resp.Body.Close()

	status := resp.StatusCode
	switch {
	case status >= 200 && status < 300:
		return Result{
			StatusCode: status,
			Ack:        true,
		}, nil

	case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500:
		return Result{
			StatusCode:      status,
			Ack:             false,
			ResponseSnippet: snippet,
		}, fmt.Errorf("webhook returned retryable HTTP status %d", status)

	case status >= 400 && status < 500:
		if f.ackOn4xx {
			return Result{
				StatusCode:      status,
				Ack:             true,
				Acked4xx:        true,
				ResponseSnippet: snippet,
			}, nil
		}
		return Result{
			StatusCode:      status,
			Ack:             false,
			ResponseSnippet: snippet,
		}, fmt.Errorf("webhook returned HTTP status %d", status)

	default:
		return Result{
			StatusCode:      status,
			Ack:             false,
			ResponseSnippet: snippet,
		}, fmt.Errorf("webhook returned unexpected HTTP status %d", status)
	}
}

func (f *Forwarder) sign(body []byte) string {
	mac := hmac.New(f.hmacHashConstructor, f.hmacSecret)
	mac.Write(body)
	return f.hmacPrefix + hex.EncodeToString(mac.Sum(nil))
}

func readSnippetAndDrain(body io.Reader) string {
	buf, _ := io.ReadAll(io.LimitReader(body, maxSnippetBytes))
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxDrainBytes))
	return sanitizeSnippet(string(buf))
}

func sanitizeSnippet(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			b.WriteByte(' ')
		} else if r >= 32 && r != 127 {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
