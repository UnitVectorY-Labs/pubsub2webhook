package consumer

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/UnitVectorY-Labs/pubsub2webhook/internal/config"
	"github.com/UnitVectorY-Labs/pubsub2webhook/internal/forwarder"
)

type stubSender struct {
	called   int
	lastMsg  forwarder.Message
	result   forwarder.Result
	err      error
}

func (s *stubSender) Forward(ctx context.Context, msg forwarder.Message) (forwarder.Result, error) {
	s.called++
	s.lastMsg = msg
	return s.result, s.err
}

func testConfig() *config.Config {
	return &config.Config{
		PubSubSubscription:          "projects/p/subscriptions/s",
		PayloadCompressionAttribute: "compression",
		MaxPayloadBytes:             1024,
	}
}

func TestProcessorFlow(t *testing.T) {
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	defer enc.Close()

	plain := []byte(`{"action":"pushed"}`)
	compressed := enc.EncodeAll(plain, nil)
	now := time.Now().UTC()

	t.Run("decompress zstd and ack on 200", func(t *testing.T) {
		sender := &stubSender{result: forwarder.Result{StatusCode: http.StatusOK, Ack: true}}
		proc := NewProcessor(testConfig(), sender)

		ack := proc.Process(context.Background(), forwarder.Message{
			ID:          "m1",
			PublishTime: now,
			Data:        compressed,
			Attributes:  map[string]string{"compression": "zstd", "gh_event": "push"},
		})
		if !ack {
			t.Fatalf("expected Ack=true")
		}
		if sender.called != 1 {
			t.Fatalf("sender.called = %d, want 1", sender.called)
		}
		if string(sender.lastMsg.Data) != string(plain) {
			t.Errorf("forwarded data = %q, want %q", sender.lastMsg.Data, plain)
		}
	})

	t.Run("nack on retryable webhook failure", func(t *testing.T) {
		sender := &stubSender{
			result: forwarder.Result{StatusCode: http.StatusServiceUnavailable, Ack: false},
			err:    errors.New("503"),
		}
		proc := NewProcessor(testConfig(), sender)

		ack := proc.Process(context.Background(), forwarder.Message{
			ID:   "m2",
			Data: plain,
		})
		if ack {
			t.Fatalf("expected Ack=false (Nack)")
		}
		if sender.called != 1 {
			t.Fatalf("sender.called = %d, want 1", sender.called)
		}
	})

	t.Run("ack 4xx when forwarder marks Acked4xx", func(t *testing.T) {
		sender := &stubSender{
			result: forwarder.Result{StatusCode: http.StatusBadRequest, Ack: true, Acked4xx: true, ResponseSnippet: "bad"},
		}
		proc := NewProcessor(testConfig(), sender)

		ack := proc.Process(context.Background(), forwarder.Message{
			ID:   "m3",
			Data: plain,
		})
		if !ack {
			t.Fatalf("expected Ack=true")
		}
	})

	t.Run("corrupt compressed payload acks without calling webhook", func(t *testing.T) {
		sender := &stubSender{result: forwarder.Result{StatusCode: http.StatusOK, Ack: true}}
		proc := NewProcessor(testConfig(), sender)

		ack := proc.Process(context.Background(), forwarder.Message{
			ID:         "m4",
			Data:       []byte("corrupt-bytes"),
			Attributes: map[string]string{"compression": "zstd"},
		})
		if !ack {
			t.Fatalf("expected Ack=true for poison message")
		}
		if sender.called != 0 {
			t.Fatalf("expected webhook not to be called, called=%d", sender.called)
		}
	})
}
