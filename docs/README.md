---
layout: default
title: pubsub2webhook
nav_order: 1
permalink: /
---

# pubsub2webhook Documentation

Pulls GCP Pub/Sub messages and replays them as HTTP webhook requests.

`pubsub2webhook` consumes messages from a single Google Cloud Pub/Sub pull subscription, optionally decompresses payloads (`gzip` or `zstd`), maps Pub/Sub attributes and static values to HTTP headers, optionally signs the outgoing request body with HMAC, and forwards each message to a configured HTTP or HTTPS endpoint.

## Guides

- [Installation](INSTALL.md): prerequisites, Pub/Sub setup, Docker image, and building from source.
- [Configuration](CONFIGURATION.md): environment variables, CLI flags, header mapping, HMAC signing, decompression, and Ack/Nack behavior.
- [Usage](USAGE.md): validate configuration offline, run the service, check health endpoints, and inspect structured logs.
- [Examples](EXAMPLES.md): Kubernetes deployment and replaying `ghook2pubsub` webhooks to Argo CD.

## Related projects

- [ghook2pubsub](https://github.com/UnitVectorY-Labs/ghook2pubsub) receives GitHub webhooks, verifies signatures, and publishes events to Google Cloud Pub/Sub.
- [pubsub2postgresaudit](https://github.com/UnitVectorY-Labs/pubsub2postgresaudit) consumes Pub/Sub messages and stores an audit history in PostgreSQL.
- [ghook4action](https://github.com/UnitVectorY-Labs/ghook4action) consumes GitHub events from Pub/Sub to manage GitHub organization Projects and cross-repository workflow dispatches.

Use a dedicated Pub/Sub subscription for each downstream consumer so every service receives its own copy of the event stream.
