---
layout: default
title: Configuration
nav_order: 3
permalink: /configuration
---

# Configuration

Every runtime setting can be configured with an environment variable or a CLI flag. Environment variables provide the defaults, and explicit CLI flags override them.

## Reference

| Environment Variable | CLI Flag | Default | Description |
|----------------------|----------|---------|-------------|
| `PUBSUB_SUBSCRIPTION` | `--pubsub-subscription` | *(required)* | Full Pub/Sub subscription resource name: `projects/<project>/subscriptions/<sub>`. |
| `WEBHOOK_URL` | `--webhook-url` | *(required)* | Target HTTP or HTTPS webhook URL (must have `http` or `https` scheme and non-empty host). |
| `WEBHOOK_METHOD` | `--webhook-method` | `POST` | Outgoing HTTP method (non-empty uppercase HTTP token). |
| `WEBHOOK_CONTENT_TYPE` | `--webhook-content-type` | `application/json` | Default `Content-Type` header if not overridden by static or attribute headers. |
| `WEBHOOK_TIMEOUT` | `--webhook-timeout` | `10s` | Per-request HTTP timeout (Go duration syntax, must be `> 0`). |
| `WEBHOOK_ATTRIBUTE_HEADERS` | `--webhook-attribute-headers` | *(empty)* | Maps Pub/Sub message attributes to outgoing HTTP headers. Accepts `attr1=Header-1,attr2=Header-2` or a JSON object `{"attr1":"Header-1"}`. |
| `WEBHOOK_STATIC_HEADERS` | `--webhook-static-headers` | *(empty)* | Static HTTP headers sent on every request. Accepts `Header-1=Value1,Header-2=Value2` or a JSON object `{"Header-1":"Value1"}`. |
| `WEBHOOK_INCLUDE_PUBSUB_HEADERS` | `--webhook-include-pubsub-headers` | `false` | When `true`, adds `X-PubSub-Message-ID` and `X-PubSub-Publish-Time` (RFC3339 UTC) headers. |
| `WEBHOOK_HMAC_SECRET` | `--webhook-hmac-secret` | *(empty)* | Shared secret used to sign the outgoing (decompressed) request body. Mutually exclusive with `WEBHOOK_HMAC_SECRET_FILE`. |
| `WEBHOOK_HMAC_SECRET_FILE` | `--webhook-hmac-secret-file` | *(empty)* | Path to a file containing the HMAC shared secret. Trailing `\r` and `\n` characters are trimmed. Mutually exclusive with `WEBHOOK_HMAC_SECRET`. |
| `WEBHOOK_HMAC_HEADER` | `--webhook-hmac-header` | `X-Hub-Signature-256` | HTTP header name for the computed HMAC signature. |
| `WEBHOOK_HMAC_ALGORITHM` | `--webhook-hmac-algorithm` | `sha256` | HMAC hash algorithm: `sha256`, `sha1`, or `sha512`. |
| `WEBHOOK_HMAC_PREFIX` | `--webhook-hmac-prefix` | `<algorithm>=` | Prefix prepended to the hex-encoded HMAC digest (for example `sha256=`). Set to an empty string (`""`) to emit raw hex with no prefix. |
| `WEBHOOK_CA_FILE` | `--webhook-ca-file` | *(empty)* | Path to a PEM CA certificate bundle appended to the system root pool for HTTPS verification. |
| `WEBHOOK_ACK_ON_4XX` | `--webhook-ack-on-4xx` | `true` | Acknowledge non-retryable `4xx` responses (excluding `408` and `429`, which always Nack). |
| `PAYLOAD_COMPRESSION_ATTRIBUTE` | `--payload-compression-attribute` | `compression` | Pub/Sub attribute indicating payload compression (`gzip`, `zstd`, `none`, or absent). Set to an empty string (`""`) to disable decompression. |
| `MAX_PAYLOAD_BYTES` | `--max-payload-bytes` | `26214400` (25 MiB) | Maximum allowed decompressed payload size in bytes. |
| `PUBSUB_MAX_OUTSTANDING_MESSAGES` | `--pubsub-max-outstanding-messages` | `10` | Maximum number of unprocessed Pub/Sub messages held in memory. |
| `PUBSUB_MAX_OUTSTANDING_BYTES` | `--pubsub-max-outstanding-bytes` | `10485760` (10 MiB) | Maximum number of unprocessed Pub/Sub message bytes held in memory. |
| `PUBSUB_NUM_GOROUTINES` | `--pubsub-num-goroutines` | `1` | Number of streaming pull goroutines for the Pub/Sub subscriber. |
| `HEALTH_PORT` | `--health-port` | `8080` | TCP port for `/healthz` and `/readyz`. Set to `""` or `0` to disable the health server. |
| `LOG_LEVEL` | `--log-level` | `info` | Structured JSON log level: `debug`, `info`, `warn`, or `error`. |

### CLI-only flags

- `--version` / `-v`: print the build version and exit `0`.
- `--check-config`: parse and validate all settings (including reading `WEBHOOK_HMAC_SECRET_FILE` and `WEBHOOK_CA_FILE` if configured) without making network requests, print `configuration valid`, and exit `0`.

## Header precedence

Outgoing HTTP headers are applied in the following order (later steps override earlier ones):

1. `Content-Type` from `WEBHOOK_CONTENT_TYPE` (if non-empty).
2. `WEBHOOK_STATIC_HEADERS`.
3. `WEBHOOK_ATTRIBUTE_HEADERS` (applied only when the Pub/Sub message contains a non-empty value for that attribute).
4. `X-PubSub-Message-ID` and `X-PubSub-Publish-Time` when `WEBHOOK_INCLUDE_PUBSUB_HEADERS=true`.
5. The HMAC signature header (`WEBHOOK_HMAC_HEADER`) when HMAC signing is enabled, ensuring computed signatures cannot be overwritten by static or attribute headers.

Use the JSON object format for `WEBHOOK_STATIC_HEADERS` or `WEBHOOK_ATTRIBUTE_HEADERS` whenever a header value contains commas (for example `{"Accept":"application/json, text/plain"}`).

## Payload decompression

When `PAYLOAD_COMPRESSION_ATTRIBUTE` is non-empty (default: `compression`), `pubsub2webhook` inspects that Pub/Sub message attribute:

- Absent, empty, or `none`: payload is forwarded as-is (subject to `MAX_PAYLOAD_BYTES`).
- `gzip`: decompressed with standard `compress/gzip` up to `MAX_PAYLOAD_BYTES`.
- `zstd`: decompressed with `github.com/klauspost/compress/zstd` (capped at 64 MiB decoder memory and `MAX_PAYLOAD_BYTES` output).
- Any other value, corrupt compressed data, or payloads exceeding `MAX_PAYLOAD_BYTES`: logged as a warning and **acknowledged** without calling the webhook so poison messages do not block the subscription.

Set `PAYLOAD_COMPRESSION_ATTRIBUTE=""` (or `--payload-compression-attribute=""`) to forward raw bytes without inspecting any compression attribute.

## HMAC signing

When either `WEBHOOK_HMAC_SECRET` or `WEBHOOK_HMAC_SECRET_FILE` is configured, `pubsub2webhook` computes the hex-encoded HMAC digest over the outgoing (decompressed) request body bytes and sets `WEBHOOK_HMAC_HEADER` to `<WEBHOOK_HMAC_PREFIX><hex-digest>`.

When reading from `WEBHOOK_HMAC_SECRET_FILE`, trailing `\r` and `\n` characters are stripped so Kubernetes Secret volume mounts work without formatting issues. Neither the secret nor the computed signature is ever written to logs.

## Ack and Nack semantics

| Outcome | Action | Notes |
|---------|--------|-------|
| HTTP `2xx` | **Ack** | Delivery succeeded. |
| HTTP `408` Request Timeout, `429` Too Many Requests | **Nack** | Always retried regardless of `WEBHOOK_ACK_ON_4XX`. |
| HTTP `5xx` | **Nack** | Always retried. |
| Network, DNS, TLS, or timeout error | **Nack** | Always retried. |
| Other HTTP `4xx` | **Ack** (if `WEBHOOK_ACK_ON_4XX=true`) / **Nack** (if `false`) | Default `true` prevents permanent client errors from causing infinite retry loops. |
| Decompression error or payload exceeds `MAX_PAYLOAD_BYTES` | **Ack** | Poison message is not forwarded to the webhook. |
