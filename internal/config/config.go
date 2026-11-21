// Package config parses and validates runtime configuration from environment
// variables and CLI flags.
package config

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultWebhookMethod                = "POST"
	DefaultWebhookContentType           = "application/json"
	DefaultWebhookTimeout               = 10 * time.Second
	DefaultWebhookHMACHeader            = "X-Hub-Signature-256"
	DefaultWebhookHMACAlgorithm         = "sha256"
	DefaultPayloadCompressionAttribute  = "compression"
	DefaultMaxPayloadBytes              = int64(25 << 20) // 25 MiB (26214400)
	DefaultPubSubMaxOutstandingMessages = 10
	DefaultPubSubMaxOutstandingBytes    = 10 << 20 // 10 MiB (10485760)
	DefaultPubSubNumGoroutines          = 1
	DefaultHealthPort                   = "8080"
	DefaultLogLevel                     = "info"
)

// Meta captures non-runtime CLI flags such as --version and --check-config.
type Meta struct {
	ShowVersion bool
	CheckConfig bool
}

// Config holds all runtime settings for pubsub2webhook.
type Config struct {
	PubSubSubscription           string
	WebhookURL                   string
	WebhookMethod                string
	WebhookContentType           string
	WebhookTimeout               time.Duration
	WebhookAttributeHeaders      map[string]string
	WebhookStaticHeaders         map[string]string
	WebhookIncludePubSubHeaders  bool
	WebhookHMACSecret            string
	WebhookHMACSecretFile        string
	ResolvedHMACSecret           []byte
	WebhookHMACHeader            string
	WebhookHMACAlgorithm         string
	WebhookHMACPrefix            string
	hmacPrefixExplicit           bool
	WebhookCAFile                string
	WebhookAckOn4xx              bool
	PayloadCompressionAttribute  string
	MaxPayloadBytes              int64
	PubSubMaxOutstandingMessages int
	PubSubMaxOutstandingBytes    int
	PubSubNumGoroutines          int
	HealthPort                   string
	LogLevel                     string
}

// LookupEnv matches os.LookupEnv.
type LookupEnv func(key string) (string, bool)

// Parse parses configuration from environment variables (defaults) and CLI
// arguments (overrides).
func Parse(args []string, lookupEnv LookupEnv) (*Config, *Meta, error) {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}

	fs := flag.NewFlagSet("pubsub2webhook", flag.ContinueOnError)

	envStr := func(key, fallback string) string {
		if v, ok := lookupEnv(key); ok && v != "" {
			return v
		}
		return fallback
	}
	envStrAllowEmpty := func(key, fallback string) (string, bool) {
		if v, ok := lookupEnv(key); ok {
			return v, true
		}
		return fallback, false
	}

	envErrors := map[string]error{}

	envBool := func(flagName, envKey string, fallback bool) bool {
		v, ok := lookupEnv(envKey)
		if !ok || v == "" {
			return fallback
		}
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			envErrors[flagName] = fmt.Errorf("invalid %s value %q: %w", envKey, v, err)
			return fallback
		}
		return b
	}

	envInt := func(flagName, envKey string, fallback int) int {
		v, ok := lookupEnv(envKey)
		if !ok || v == "" {
			return fallback
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			envErrors[flagName] = fmt.Errorf("invalid %s value %q: %w", envKey, v, err)
			return fallback
		}
		return n
	}

	envInt64 := func(flagName, envKey string, fallback int64) int64 {
		v, ok := lookupEnv(envKey)
		if !ok || v == "" {
			return fallback
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			envErrors[flagName] = fmt.Errorf("invalid %s value %q: %w", envKey, v, err)
			return fallback
		}
		return n
	}

	envDuration := func(flagName, envKey string, fallback time.Duration) time.Duration {
		v, ok := lookupEnv(envKey)
		if !ok || v == "" {
			return fallback
		}
		d, err := time.ParseDuration(strings.TrimSpace(v))
		if err != nil {
			envErrors[flagName] = fmt.Errorf("invalid %s value %q: %w", envKey, v, err)
			return fallback
		}
		return d
	}

	compAttrDefault, _ := envStrAllowEmpty("PAYLOAD_COMPRESSION_ATTRIBUTE", DefaultPayloadCompressionAttribute)
	healthPortDefault, _ := envStrAllowEmpty("HEALTH_PORT", DefaultHealthPort)
	hmacPrefixDefault, hmacPrefixFromEnv := envStrAllowEmpty("WEBHOOK_HMAC_PREFIX", "")

	showVersionLong := fs.Bool("version", false, "Print version and exit")
	showVersionShort := fs.Bool("v", false, "Print version and exit (shorthand)")
	checkConfig := fs.Bool("check-config", false, "Validate configuration and exit without network access")

	pubsubSub := fs.String("pubsub-subscription", envStr("PUBSUB_SUBSCRIPTION", ""), "Full Pub/Sub subscription name (projects/<project>/subscriptions/<sub>)")
	webhookURL := fs.String("webhook-url", envStr("WEBHOOK_URL", ""), "Target HTTP/HTTPS webhook URL")
	webhookMethod := fs.String("webhook-method", envStr("WEBHOOK_METHOD", DefaultWebhookMethod), "HTTP method for outgoing webhook requests")
	webhookContentType := fs.String("webhook-content-type", envStr("WEBHOOK_CONTENT_TYPE", DefaultWebhookContentType), "Default Content-Type header when not set by static or attribute headers")
	webhookTimeout := fs.Duration("webhook-timeout", envDuration("webhook-timeout", "WEBHOOK_TIMEOUT", DefaultWebhookTimeout), "HTTP request timeout")
	webhookAttrHeadersRaw := fs.String("webhook-attribute-headers", envStr("WEBHOOK_ATTRIBUTE_HEADERS", ""), "Map Pub/Sub attributes to HTTP headers (attr=Header,... or JSON object)")
	webhookStaticHeadersRaw := fs.String("webhook-static-headers", envStr("WEBHOOK_STATIC_HEADERS", ""), "Static HTTP headers to send on every request (Header=Value,... or JSON object)")
	webhookIncludePubSubHeaders := fs.Bool("webhook-include-pubsub-headers", envBool("webhook-include-pubsub-headers", "WEBHOOK_INCLUDE_PUBSUB_HEADERS", false), "Include X-PubSub-Message-ID and X-PubSub-Publish-Time headers")
	webhookHMACSecret := fs.String("webhook-hmac-secret", envStr("WEBHOOK_HMAC_SECRET", ""), "HMAC secret value for signing outgoing request bodies")
	webhookHMACSecretFile := fs.String("webhook-hmac-secret-file", envStr("WEBHOOK_HMAC_SECRET_FILE", ""), "Path to file containing HMAC secret (mutually exclusive with --webhook-hmac-secret)")
	webhookHMACHeader := fs.String("webhook-hmac-header", envStr("WEBHOOK_HMAC_HEADER", DefaultWebhookHMACHeader), "HTTP header name for the computed HMAC signature")
	webhookHMACAlgo := fs.String("webhook-hmac-algorithm", envStr("WEBHOOK_HMAC_ALGORITHM", DefaultWebhookHMACAlgorithm), "HMAC hash algorithm: sha256, sha1, or sha512")
	webhookHMACPrefix := fs.String("webhook-hmac-prefix", hmacPrefixDefault, "Prefix prepended to the hex HMAC digest (default: <algorithm>=; set empty for raw hex)")
	webhookCAFile := fs.String("webhook-ca-file", envStr("WEBHOOK_CA_FILE", ""), "Path to additional PEM CA certificate bundle")
	webhookAckOn4xx := fs.Bool("webhook-ack-on-4xx", envBool("webhook-ack-on-4xx", "WEBHOOK_ACK_ON_4XX", true), "Acknowledge non-retryable 4xx responses (408 and 429 always Nack)")
	payloadCompAttr := fs.String("payload-compression-attribute", compAttrDefault, "Pub/Sub attribute indicating payload compression (empty disables decompression)")
	maxPayloadBytes := fs.Int64("max-payload-bytes", envInt64("max-payload-bytes", "MAX_PAYLOAD_BYTES", DefaultMaxPayloadBytes), "Maximum decompressed payload size in bytes")
	maxOutstandingMessages := fs.Int("pubsub-max-outstanding-messages", envInt("pubsub-max-outstanding-messages", "PUBSUB_MAX_OUTSTANDING_MESSAGES", DefaultPubSubMaxOutstandingMessages), "Maximum unprocessed Pub/Sub messages")
	maxOutstandingBytes := fs.Int("pubsub-max-outstanding-bytes", envInt("pubsub-max-outstanding-bytes", "PUBSUB_MAX_OUTSTANDING_BYTES", DefaultPubSubMaxOutstandingBytes), "Maximum unprocessed Pub/Sub bytes")
	numGoroutines := fs.Int("pubsub-num-goroutines", envInt("pubsub-num-goroutines", "PUBSUB_NUM_GOROUTINES", DefaultPubSubNumGoroutines), "Number of Pub/Sub streaming pull goroutines")
	healthPort := fs.String("health-port", healthPortDefault, "Port for /healthz and /readyz endpoints (empty or 0 disables)")
	logLevel := fs.String("log-level", envStr("LOG_LEVEL", DefaultLogLevel), "Log level: debug, info, warn, or error")

	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	if fs.NArg() != 0 {
		return nil, nil, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}

	meta := &Meta{
		ShowVersion: *showVersionLong || *showVersionShort,
		CheckConfig: *checkConfig,
	}
	if meta.ShowVersion {
		return &Config{}, meta, nil
	}

	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		visited[f.Name] = true
	})

	for flagName, err := range envErrors {
		if !visited[flagName] {
			return nil, nil, err
		}
	}

	attrHeaders, err := ParseHeaderMap(*webhookAttrHeadersRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid WEBHOOK_ATTRIBUTE_HEADERS: %w", err)
	}
	staticHeaders, err := ParseHeaderMap(*webhookStaticHeadersRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid WEBHOOK_STATIC_HEADERS: %w", err)
	}

	cfg := &Config{
		PubSubSubscription:           strings.TrimSpace(*pubsubSub),
		WebhookURL:                   strings.TrimSpace(*webhookURL),
		WebhookMethod:                strings.TrimSpace(*webhookMethod),
		WebhookContentType:           strings.TrimSpace(*webhookContentType),
		WebhookTimeout:               *webhookTimeout,
		WebhookAttributeHeaders:      attrHeaders,
		WebhookStaticHeaders:         staticHeaders,
		WebhookIncludePubSubHeaders:  *webhookIncludePubSubHeaders,
		WebhookHMACSecret:            *webhookHMACSecret,
		WebhookHMACSecretFile:        strings.TrimSpace(*webhookHMACSecretFile),
		WebhookHMACHeader:            strings.TrimSpace(*webhookHMACHeader),
		WebhookHMACAlgorithm:         strings.ToLower(strings.TrimSpace(*webhookHMACAlgo)),
		WebhookHMACPrefix:            *webhookHMACPrefix,
		hmacPrefixExplicit:           visited["webhook-hmac-prefix"] || hmacPrefixFromEnv,
		WebhookCAFile:                strings.TrimSpace(*webhookCAFile),
		WebhookAckOn4xx:              *webhookAckOn4xx,
		PayloadCompressionAttribute:  strings.TrimSpace(*payloadCompAttr),
		MaxPayloadBytes:              *maxPayloadBytes,
		PubSubMaxOutstandingMessages: *maxOutstandingMessages,
		PubSubMaxOutstandingBytes:    *maxOutstandingBytes,
		PubSubNumGoroutines:          *numGoroutines,
		HealthPort:                   strings.TrimSpace(*healthPort),
		LogLevel:                     strings.ToLower(strings.TrimSpace(*logLevel)),
	}

	return cfg, meta, nil
}

// Validate checks all configuration fields and resolves secret files and defaults.
func (c *Config) Validate() error {
	if _, _, err := ParseSubscription(c.PubSubSubscription); err != nil {
		return err
	}

	if err := validateWebhookURL(c.WebhookURL); err != nil {
		return err
	}

	if !isValidHTTPMethod(c.WebhookMethod) {
		return fmt.Errorf("WEBHOOK_METHOD must be a non-empty uppercase HTTP method token, got %q", c.WebhookMethod)
	}

	if containsCRLF(c.WebhookContentType) {
		return errors.New("WEBHOOK_CONTENT_TYPE must not contain CR or LF characters")
	}

	if c.WebhookTimeout <= 0 {
		return fmt.Errorf("WEBHOOK_TIMEOUT must be greater than 0, got %s", c.WebhookTimeout)
	}

	for attr, header := range c.WebhookAttributeHeaders {
		if strings.TrimSpace(attr) == "" {
			return errors.New("WEBHOOK_ATTRIBUTE_HEADERS contains an empty attribute key")
		}
		if !isValidHeaderName(header) {
			return fmt.Errorf("WEBHOOK_ATTRIBUTE_HEADERS maps %q to invalid HTTP header name %q", attr, header)
		}
	}

	for header, val := range c.WebhookStaticHeaders {
		if !isValidHeaderName(header) {
			return fmt.Errorf("WEBHOOK_STATIC_HEADERS contains invalid HTTP header name %q", header)
		}
		if containsCRLF(val) {
			return fmt.Errorf("WEBHOOK_STATIC_HEADERS value for %q must not contain CR or LF characters", header)
		}
	}

	if c.WebhookHMACSecret != "" && c.WebhookHMACSecretFile != "" {
		return errors.New("WEBHOOK_HMAC_SECRET and WEBHOOK_HMAC_SECRET_FILE are mutually exclusive")
	}

	c.WebhookHMACAlgorithm = strings.ToLower(strings.TrimSpace(c.WebhookHMACAlgorithm))
	switch c.WebhookHMACAlgorithm {
	case "sha256", "sha1", "sha512":
	default:
		return fmt.Errorf("WEBHOOK_HMAC_ALGORITHM must be one of sha256, sha1, sha512, got %q", c.WebhookHMACAlgorithm)
	}

	if !c.hmacPrefixExplicit && c.WebhookHMACPrefix == "" {
		c.WebhookHMACPrefix = c.WebhookHMACAlgorithm + "="
	}
	if containsCRLF(c.WebhookHMACPrefix) {
		return errors.New("WEBHOOK_HMAC_PREFIX must not contain CR or LF characters")
	}

	if !isValidHeaderName(c.WebhookHMACHeader) {
		return fmt.Errorf("WEBHOOK_HMAC_HEADER must be a valid HTTP header name, got %q", c.WebhookHMACHeader)
	}

	c.ResolvedHMACSecret = nil
	if c.WebhookHMACSecret != "" {
		c.ResolvedHMACSecret = []byte(c.WebhookHMACSecret)
	} else if c.WebhookHMACSecretFile != "" {
		raw, err := os.ReadFile(c.WebhookHMACSecretFile)
		if err != nil {
			return fmt.Errorf("reading WEBHOOK_HMAC_SECRET_FILE: %w", err)
		}
		trimmed := strings.TrimRight(string(raw), "\r\n")
		if trimmed == "" {
			return errors.New("WEBHOOK_HMAC_SECRET_FILE is empty")
		}
		c.ResolvedHMACSecret = []byte(trimmed)
	}

	if c.WebhookCAFile != "" {
		pemData, err := os.ReadFile(c.WebhookCAFile)
		if err != nil {
			return fmt.Errorf("reading WEBHOOK_CA_FILE: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemData) {
			return fmt.Errorf("WEBHOOK_CA_FILE %q contains no valid PEM certificates", c.WebhookCAFile)
		}
	}

	if c.MaxPayloadBytes <= 0 {
		return fmt.Errorf("MAX_PAYLOAD_BYTES must be greater than 0, got %d", c.MaxPayloadBytes)
	}

	if c.PubSubMaxOutstandingMessages <= 0 {
		return fmt.Errorf("PUBSUB_MAX_OUTSTANDING_MESSAGES must be greater than 0, got %d", c.PubSubMaxOutstandingMessages)
	}

	if c.PubSubMaxOutstandingBytes <= 0 {
		return fmt.Errorf("PUBSUB_MAX_OUTSTANDING_BYTES must be greater than 0, got %d", c.PubSubMaxOutstandingBytes)
	}

	if c.PubSubNumGoroutines <= 0 {
		return fmt.Errorf("PUBSUB_NUM_GOROUTINES must be greater than 0, got %d", c.PubSubNumGoroutines)
	}

	if c.HealthEnabled() {
		portNum, err := strconv.Atoi(c.HealthPort)
		if err != nil || portNum < 1 || portNum > 65535 {
			return fmt.Errorf("HEALTH_PORT must be empty, 0, or a valid TCP port (1-65535), got %q", c.HealthPort)
		}
	}

	c.LogLevel = strings.ToLower(strings.TrimSpace(c.LogLevel))
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LOG_LEVEL must be one of debug, info, warn, error, got %q", c.LogLevel)
	}

	return nil
}

// HealthEnabled reports whether the HTTP health server should be started.
func (c *Config) HealthEnabled() bool {
	return c.HealthPort != "" && c.HealthPort != "0"
}

// SlogLevel returns the slog.Level corresponding to Config.LogLevel.
func (c *Config) SlogLevel() slog.Level {
	switch strings.ToLower(strings.TrimSpace(c.LogLevel)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ParseSubscription extracts the project ID and subscription ID from a full
// Pub/Sub subscription resource name of the form
// "projects/<project>/subscriptions/<sub>".
func ParseSubscription(full string) (projectID, subscriptionID string, err error) {
	if strings.TrimSpace(full) == "" {
		return "", "", errors.New("PUBSUB_SUBSCRIPTION is required (expected projects/<project>/subscriptions/<sub>)")
	}
	parts := strings.Split(full, "/")
	if len(parts) != 4 || parts[0] != "projects" || parts[1] == "" || parts[2] != "subscriptions" || parts[3] == "" {
		return "", "", fmt.Errorf("invalid PUBSUB_SUBSCRIPTION %q: expected projects/<project>/subscriptions/<sub>", full)
	}
	if strings.ContainsAny(parts[1], " \t\r\n") || strings.ContainsAny(parts[3], " \t\r\n") {
		return "", "", fmt.Errorf("invalid PUBSUB_SUBSCRIPTION %q: project and subscription must not contain whitespace", full)
	}
	return parts[1], parts[3], nil
}

// ParseHeaderMap parses either a JSON object ({"k":"v"}) or a comma-separated
// list of key=value pairs (k1=v1,k2=v2).
func ParseHeaderMap(raw string) (map[string]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	if strings.HasPrefix(trimmed, "{") {
		var parsed map[string]string
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return nil, fmt.Errorf("parsing JSON header map: %w", err)
		}
		if len(parsed) == 0 {
			return nil, nil
		}
		out := make(map[string]string, len(parsed))
		for k, v := range parsed {
			k = strings.TrimSpace(k)
			if k == "" {
				return nil, errors.New("JSON header map contains an empty key")
			}
			out[k] = v
		}
		return out, nil
	}

	out := make(map[string]string)
	entries := strings.Split(trimmed, ",")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return nil, fmt.Errorf("empty entry in header mapping %q", raw)
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid header mapping entry %q: expected key=value", entry)
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("empty key in header mapping entry %q", entry)
		}
		out[key] = val
	}
	return out, nil
}

func validateWebhookURL(raw string) error {
	if raw == "" {
		return errors.New("WEBHOOK_URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid WEBHOOK_URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("WEBHOOK_URL scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("WEBHOOK_URL must include a non-empty host")
	}
	return nil
}

func isValidHTTPMethod(m string) bool {
	if m == "" {
		return false
	}
	for i := 0; i < len(m); i++ {
		c := m[i]
		if c >= 'a' && c <= 'z' {
			return false
		}
		if !isTokenChar(c) {
			return false
		}
	}
	return true
}

func isValidHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !isTokenChar(name[i]) {
			return false
		}
	}
	return true
}

// isTokenChar implements RFC 9110 tchar.
func isTokenChar(c byte) bool {
	if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}

func containsCRLF(s string) bool {
	return strings.ContainsAny(s, "\r\n\x00")
}
