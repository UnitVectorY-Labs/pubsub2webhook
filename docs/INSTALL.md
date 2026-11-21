---
layout: default
title: Installation
nav_order: 2
permalink: /install
---

# Installation

The project ID, topic name, subscription name, and webhook URL in these commands are placeholders. Replace them with your own values.

## Prerequisites

- A Google Cloud project with the Pub/Sub API enabled and an existing topic and pull subscription.
- A Google Cloud identity granted `roles/pubsub.subscriber` on that subscription. Use a dedicated subscription per consumer so replicas do not compete with other downstream services.
- A reachable HTTP or HTTPS webhook destination.
- Docker, or the Go version specified in [go.mod](../go.mod) to build from source.

Create a dedicated pull subscription if needed:

```bash
gcloud pubsub subscriptions create webhook-replay \
  --project=my-gcp-project \
  --topic=my-events-topic
```

## Container image

Development snapshots are published to GitHub Container Registry for `linux/amd64` and `linux/arm64`:

```text
ghcr.io/unitvectory-labs/pubsub2webhook-snapshot:dev
```

The `:dev` tag is mutable; pin a specific digest for controlled deployments. The container image is built on `gcr.io/distroless/base-debian13`, runs `/server` as non-root UID/GID `65532:65532`, and exposes port `8080` for health probes. Ensure any mounted secret or CA files are readable by UID `65532`.

Build locally with Docker from the repository root:

```bash
docker build -t pubsub2webhook .
```

Run with Docker using Application Default Credentials mounted into the container:

```bash
docker run --rm -p 8080:8080 \
  --mount type=bind,src="$HOME/.config/gcloud/application_default_credentials.json",dst=/credentials/adc.json,readonly \
  -e GOOGLE_APPLICATION_CREDENTIALS=/credentials/adc.json \
  -e PUBSUB_SUBSCRIPTION=projects/my-gcp-project/subscriptions/webhook-replay \
  -e WEBHOOK_URL=https://receiver.example.com/api/webhook \
  pubsub2webhook
```

On Google Cloud (GKE Workload Identity, Cloud Run, GCE), attach a service account with `roles/pubsub.subscriber` instead of mounting a local credential file.

## Build from source

```bash
git clone https://github.com/UnitVectorY-Labs/pubsub2webhook.git
cd pubsub2webhook
go build -o pubsub2webhook .
./pubsub2webhook --version
```

Validate your settings without network access before starting the consumer:

```bash
./pubsub2webhook \
  --pubsub-subscription projects/my-gcp-project/subscriptions/webhook-replay \
  --webhook-url https://receiver.example.com/api/webhook \
  --check-config
```

Continue with [Configuration](CONFIGURATION.md) and [Usage](USAGE.md).
