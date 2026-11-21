package main

import (
	"testing"
)

func TestRedactURL(t *testing.T) {
	got := redactURL("https://user:pass@argocd.internal:443/api/webhook?token=secret#frag")
	want := "https://argocd.internal:443/api/webhook"
	if got != want {
		t.Errorf("redactURL() = %q, want %q", got, want)
	}
}

func TestRunVersionAndCheckConfig(t *testing.T) {
	if err := run([]string{"--version"}); err != nil {
		t.Fatalf("run(--version) error: %v", err)
	}
	if err := run([]string{"-v"}); err != nil {
		t.Fatalf("run(-v) error: %v", err)
	}
	if err := run([]string{
		"--pubsub-subscription", "projects/demo/subscriptions/sub1",
		"--webhook-url", "https://example.com/api/webhook",
		"--check-config",
	}); err != nil {
		t.Fatalf("run(--check-config) error: %v", err)
	}
}
