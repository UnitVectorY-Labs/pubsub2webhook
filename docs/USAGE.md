---
layout: default
title: Usage
nav_order: 4
permalink: /usage
---

# Usage

## Validate configuration offline

Before deploying or after modifying environment variables, run `--check-config` to verify subscription syntax, webhook URL, header mappings, secret files, and CA bundles without opening any network connections:

```bash
PUBSUB_SUBSCRIPTION=projects/my-gcp-project/subscriptions/webhook-replay \
WEBHOOK_URL=https://receiver.example.com/api/webhook \
WEBHOOK_ATTRIBUTE_HEADERS='gh_event=X-GitHub-Event,gh_delivery=X-GitHub-Delivery' \
WEBHOOK_HMAC_SECRET_FILE=/secrets/webhook-secret \
./pubsub2webhook --check-config
```

Expected output:

```text
configuration valid
```

## Run the service

```bash
export PUBSUB_SUBSCRIPTION=projects/my-gcp-project/subscriptions/webhook-replay
export WEBHOOK_URL=https://receiver.example.com/api/webhook
export WEBHOOK_ATTRIBUTE_HEADERS='gh_event=X-GitHub-Event,gh_delivery=X-GitHub-Delivery'
export WEBHOOK_HMAC_SECRET_FILE=/secrets/webhook-secret
./pubsub2webhook
```

`pubsub2webhook` handles `SIGINT` and `SIGTERM` gracefully: it flips `/readyz` to `503 Service Unavailable`, cancels the Pub/Sub receive context so in-flight callbacks can finish, and shuts down the HTTP health server cleanly.

## Health and readiness probes

When `HEALTH_PORT` is enabled (default `8080`), two endpoints are exposed:

- `GET /healthz`: returns `200 OK` with `{"status":"ok"}` while the process is running.
- `GET /readyz`: returns `200 OK` with `{"status":"ready"}` once the subscriber loop is active, and `503 Service Unavailable` with `{"status":"not_ready"}` during startup and graceful shutdown.

```bash
curl --fail http://localhost:8080/healthz
curl --fail http://localhost:8080/readyz
```

Disable the health server by setting `HEALTH_PORT=0` or `HEALTH_PORT=""`.

## Structured logging

Logs are emitted as JSON to `stdout` using `log/slog`. Control verbosity with `LOG_LEVEL` (`debug`, `info`, `warn`, `error`).

Sensitive values are never logged:
- `WEBHOOK_HMAC_SECRET`, secret file contents, and computed signatures are never emitted.
- Outgoing request payloads are never emitted (only byte counts).
- Query parameters and userinfo in `WEBHOOK_URL` are redacted in startup logs.
- On non-`2xx` responses, sanitized response body snippets (truncated to 256 bytes) are logged only when `LOG_LEVEL=debug`.
