[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://opensource.org/licenses/MIT) [![Work In Progress](https://img.shields.io/badge/Status-Work%20In%20Progress-yellow)](https://guide.unitvectorylabs.com/bestpractices/status/#work-in-progress) 

# pubsub2webhook

Pulls GCP Pub/Sub messages and replays them as HTTP webhook requests.

## Overview

`pubsub2webhook` is a stateless Go service that consumes messages from a Google Cloud Pub/Sub subscription and replays them as HTTP webhook requests to a configured target URL. It supports optional payload decompression (`gzip`, `zstd`), attribute-to-header mapping, static headers, HMAC request signing, custom CA bundles, bounded concurrency, and HTTP health/readiness probes.

Key features:
- Consumes messages from a single GCP Pub/Sub pull subscription and forwards them via HTTP/HTTPS
- Transparent payload decompression (`gzip`, `zstd`, `none`) driven by a configurable Pub/Sub message attribute with strict decompressed size limits
- Maps Pub/Sub message attributes to outgoing HTTP headers and supports static headers
- Optional HMAC request signing (`sha256`, `sha1`, `sha512`) over the outgoing request body with secret value or mounted secret file support
- Explicit Ack/Nack semantics: Acks `2xx` (and non-retryable `4xx` by default), Nacks `408`, `429`, `5xx`, and network/timeout errors for retry
- Optional custom CA certificate bundle for internal HTTPS endpoints
- Built-in `/healthz` and `/readyz` endpoints and structured JSON logging with `log/slog`
- Configuration via environment variables or CLI flags, with `--check-config` offline validation

## Documentation

See the [documentation site](https://unitvectory-labs.github.io/pubsub2webhook/) or browse the guides directly:
- [Documentation index](docs/README.md)
- [Installation](docs/INSTALL.md)
- [Configuration](docs/CONFIGURATION.md)
- [Usage](docs/USAGE.md)
- [Examples](docs/EXAMPLES.md)

## Related projects

- [ghook2pubsub](https://github.com/UnitVectorY-Labs/ghook2pubsub) receives GitHub webhooks, verifies signatures, and publishes events to Google Cloud Pub/Sub; `pubsub2webhook` can replay those events to downstream webhook receivers such as Argo CD.
- [pubsub2postgresaudit](https://github.com/UnitVectorY-Labs/pubsub2postgresaudit) consumes Pub/Sub messages and stores an audit history in PostgreSQL.
- [ghook4action](https://github.com/UnitVectorY-Labs/ghook4action) consumes GitHub webhook events from Pub/Sub and automates GitHub organization Projects and cross-repository workflow dispatches.

Use separate subscriptions on the same topic so webhook replay, auditing, and GitHub automation operate independently.

## License

This project is licensed under the [MIT License](LICENSE).
