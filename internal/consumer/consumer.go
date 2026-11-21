// Package consumer pulls messages from Google Cloud Pub/Sub, decompresses
// payloads, and replays them via a webhook forwarder.
package consumer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cloud.google.com/go/pubsub"

	"github.com/UnitVectorY-Labs/pubsub2webhook/internal/config"
	"github.com/UnitVectorY-Labs/pubsub2webhook/internal/decompress"
	"github.com/UnitVectorY-Labs/pubsub2webhook/internal/forwarder"
)

// Sender forwards a prepared message to the target webhook.
type Sender interface {
	Forward(ctx context.Context, msg forwarder.Message) (forwarder.Result, error)
}

// Processor coordinates decompression and forwarding for individual messages.
type Processor struct {
	subscription         string
	compressionAttribute string
	maxPayloadBytes      int64
	sender               Sender
}

// NewProcessor constructs a Processor from Config and a Sender.
func NewProcessor(cfg *config.Config, sender Sender) *Processor {
	return &Processor{
		subscription:         cfg.PubSubSubscription,
		compressionAttribute: cfg.PayloadCompressionAttribute,
		maxPayloadBytes:      cfg.MaxPayloadBytes,
		sender:               sender,
	}
}

// Process handles a single message and returns true when the message should be
// acknowledged (Ack) or false when it should be retried (Nack).
func (p *Processor) Process(ctx context.Context, msg forwarder.Message) bool {
	started := time.Now()
	logAttrs := []any{
		"subscription", p.subscription,
		"message_id", msg.ID,
	}
	if !msg.PublishTime.IsZero() {
		logAttrs = append(logAttrs, "publish_time", msg.PublishTime.UTC().Format(time.RFC3339))
	}

	payload, err := decompress.Decompress(msg.Data, msg.Attributes, p.compressionAttribute, p.maxPayloadBytes)
	if err != nil {
		slog.Warn("failed to decompress message; acknowledging poison message",
			append(logAttrs,
				"outcome", "decompression_error",
				"raw_bytes", len(msg.Data),
				"error", err.Error(),
				"elapsed_ms", time.Since(started).Milliseconds(),
			)...,
		)
		return true
	}

	res, err := p.sender.Forward(ctx, forwarder.Message{
		ID:          msg.ID,
		PublishTime: msg.PublishTime,
		Data:        payload,
		Attributes:  msg.Attributes,
	})

	elapsedMs := time.Since(started).Milliseconds()

	if res.Ack {
		if res.Acked4xx {
			slog.Warn("webhook returned non-retryable 4xx; acknowledging message",
				append(logAttrs,
					"outcome", "ack_4xx",
					"status_code", res.StatusCode,
					"payload_bytes", len(payload),
					"elapsed_ms", elapsedMs,
				)...,
			)
			if res.ResponseSnippet != "" {
				slog.Debug("webhook 4xx response snippet",
					"message_id", msg.ID,
					"status_code", res.StatusCode,
					"response_snippet", res.ResponseSnippet,
				)
			}
		} else {
			slog.Info("webhook delivered",
				append(logAttrs,
					"outcome", "ack",
					"status_code", res.StatusCode,
					"payload_bytes", len(payload),
					"elapsed_ms", elapsedMs,
				)...,
			)
		}
		return true
	}

	errFields := append(logAttrs,
		"outcome", "retry",
		"payload_bytes", len(payload),
		"elapsed_ms", elapsedMs,
	)
	if res.StatusCode > 0 {
		errFields = append(errFields, "status_code", res.StatusCode)
	}
	if err != nil {
		errFields = append(errFields, "error", err.Error())
	}
	slog.Error("webhook delivery failed; will retry", errFields...)
	if res.ResponseSnippet != "" {
		slog.Debug("webhook error response snippet",
			"message_id", msg.ID,
			"status_code", res.StatusCode,
			"response_snippet", res.ResponseSnippet,
		)
	}
	return false
}

// Run subscribes to the configured Pub/Sub subscription and forwards messages
// until ctx is cancelled or a fatal Pub/Sub error occurs.
func Run(ctx context.Context, cfg *config.Config, sender Sender) error {
	projectID, subID, err := config.ParseSubscription(cfg.PubSubSubscription)
	if err != nil {
		return err
	}

	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return fmt.Errorf("creating pubsub client: %w", err)
	}
	defer client.Close()

	sub := client.Subscription(subID)
	sub.ReceiveSettings.MaxOutstandingMessages = cfg.PubSubMaxOutstandingMessages
	sub.ReceiveSettings.MaxOutstandingBytes = cfg.PubSubMaxOutstandingBytes
	sub.ReceiveSettings.NumGoroutines = cfg.PubSubNumGoroutines

	proc := NewProcessor(cfg, sender)

	err = sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		ack := proc.Process(ctx, forwarder.Message{
			ID:          msg.ID,
			PublishTime: msg.PublishTime,
			Data:        msg.Data,
			Attributes:  msg.Attributes,
		})
		if ack {
			msg.Ack()
		} else {
			msg.Nack()
		}
	})
	if err != nil {
		return fmt.Errorf("receiving pubsub messages: %w", err)
	}
	return nil
}
