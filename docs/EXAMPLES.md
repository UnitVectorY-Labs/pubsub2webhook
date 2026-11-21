---
layout: default
title: Examples
nav_order: 5
permalink: /examples
---

# Examples

## Replaying `ghook2pubsub` webhooks to Argo CD

A common deployment pattern is receiving GitHub webhooks through [ghook2pubsub](https://github.com/UnitVectorY-Labs/ghook2pubsub) (with `zstd` payload compression enabled) and replaying `push` events to an internal Argo CD server's `/api/webhook` endpoint without exposing Argo CD directly to the public internet.

### 1. Create a filtered Pub/Sub subscription

Create a dedicated pull subscription on the `ghook2pubsub` topic that only delivers `push` events:

```bash
gcloud pubsub subscriptions create argocd-webhook-replay \
  --project=my-gcp-project \
  --topic=github-webhooks \
  --message-filter='attributes.gh_event = "push"'
```

### 2. Configure the Argo CD webhook secret

Configure `webhook.github.secret` in Argo CD's `argocd-secret` Secret, and mount that same secret value into the `pubsub2webhook` pod at `/secrets/webhook-secret`.

### 3. Deploy `pubsub2webhook` on Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pubsub2webhook-argocd
  namespace: argocd
spec:
  replicas: 1
  selector:
    matchLabels:
      app: pubsub2webhook-argocd
  template:
    metadata:
      labels:
        app: pubsub2webhook-argocd
    spec:
      serviceAccountName: pubsub2webhook-argocd
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        runAsGroup: 65532
      containers:
        - name: pubsub2webhook
          image: ghcr.io/unitvectory-labs/pubsub2webhook-snapshot:dev
          ports:
            - name: health
              containerPort: 8080
          env:
            - name: PUBSUB_SUBSCRIPTION
              value: projects/my-gcp-project/subscriptions/argocd-webhook-replay
            - name: WEBHOOK_URL
              value: https://argocd-server.argocd.svc.cluster.local/api/webhook
            - name: WEBHOOK_METHOD
              value: POST
            - name: WEBHOOK_CONTENT_TYPE
              value: application/json
            - name: WEBHOOK_TIMEOUT
              value: 10s
            - name: WEBHOOK_ATTRIBUTE_HEADERS
              value: gh_event=X-GitHub-Event,gh_delivery=X-GitHub-Delivery
            - name: WEBHOOK_HMAC_SECRET_FILE
              value: /secrets/webhook-secret
            - name: WEBHOOK_HMAC_HEADER
              value: X-Hub-Signature-256
            - name: WEBHOOK_HMAC_ALGORITHM
              value: sha256
            - name: WEBHOOK_CA_FILE
              value: /certs/ca.crt
            - name: PAYLOAD_COMPRESSION_ATTRIBUTE
              value: compression
            - name: WEBHOOK_ACK_ON_4XX
              value: "true"
            - name: HEALTH_PORT
              value: "8080"
            - name: LOG_LEVEL
              value: info
          volumeMounts:
            - name: webhook-secret
              mountPath: /secrets
              readOnly: true
            - name: internal-ca
              mountPath: /certs
              readOnly: true
          livenessProbe:
            httpGet:
              path: /healthz
              port: health
            initialDelaySeconds: 5
            periodSeconds: 15
          readinessProbe:
            httpGet:
              path: /readyz
              port: health
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            requests:
              cpu: 25m
              memory: 32Mi
            limits:
              memory: 128Mi
      volumes:
        - name: webhook-secret
          secret:
            secretName: argocd-webhook-secret
            items:
              - key: webhook.github.secret
                path: webhook-secret
        - name: internal-ca
          configMap:
            name: cluster-ca-bundle
            items:
              - key: ca.crt
                path: ca.crt
```

In this setup:
- `ghook2pubsub` publishes `zstd`-compressed GitHub webhook payloads with `compression=zstd`, `gh_event`, and `gh_delivery` attributes.
- `pubsub2webhook` pulls each matching message, decompresses the `zstd` payload, maps `gh_event` to `X-GitHub-Event` and `gh_delivery` to `X-GitHub-Delivery`, signs the decompressed JSON body with `X-Hub-Signature-256: sha256=<hex>`, verifies the internal TLS certificate against `/certs/ca.crt`, and POSTs the webhook to Argo CD's `/api/webhook`.

## Static headers and JSON header mapping

When forwarding to endpoints that require a static bearer token or header values containing commas, use the JSON object syntax:

```bash
export PUBSUB_SUBSCRIPTION=projects/my-gcp-project/subscriptions/custom-webhook
export WEBHOOK_URL=https://api.internal.example.com/hooks/events
export WEBHOOK_STATIC_HEADERS='{"X-Source":"pubsub2webhook","Accept":"application/json, text/plain"}'
export WEBHOOK_ATTRIBUTE_HEADERS='{"event_type":"X-Event-Type","correlation_id":"X-Correlation-ID"}'
export WEBHOOK_INCLUDE_PUBSUB_HEADERS=true
./pubsub2webhook
```
