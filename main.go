package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/UnitVectorY-Labs/pubsub2webhook/internal/config"
	"github.com/UnitVectorY-Labs/pubsub2webhook/internal/consumer"
	"github.com/UnitVectorY-Labs/pubsub2webhook/internal/forwarder"
	"github.com/UnitVectorY-Labs/pubsub2webhook/internal/health"
)

// Version is set at build time via ldflags.
var Version = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(os.Args[1:]); err != nil {
		slog.Error("application stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-v") {
		fmt.Println(Version)
		return nil
	}

	cfg, meta, err := config.Parse(args, os.LookupEnv)
	if err != nil {
		return err
	}
	if meta.ShowVersion {
		fmt.Println(Version)
		return nil
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.SlogLevel(),
	})))

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	fwd, err := forwarder.New(cfg)
	if err != nil {
		return fmt.Errorf("forwarder initialization error: %w", err)
	}

	if meta.CheckConfig {
		fmt.Println("configuration valid")
		return nil
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var healthSrv *health.Server
	var healthErrs <-chan error
	if cfg.HealthEnabled() {
		healthSrv = health.New()
		if err := healthSrv.Start(cfg.HealthPort); err != nil {
			return fmt.Errorf("health server startup error: %w", err)
		}
		healthErrs = healthSrv.Errors()
		slog.Info("health server started", "port", cfg.HealthPort)
		defer func() {
			healthSrv.SetReady(false)
			shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if err := healthSrv.Shutdown(shutdownCtx); err != nil {
				slog.Warn("health server shutdown error", "error", err.Error())
			}
		}()

		go func() {
			select {
			case err, ok := <-healthErrs:
				if ok && err != nil {
					slog.Error("health server failed", "error", err.Error())
					cancel()
				}
			case <-ctx.Done():
			}
		}()

		healthSrv.SetReady(true)
	}

	slog.Info("starting pubsub2webhook",
		"version", Version,
		"subscription", cfg.PubSubSubscription,
		"webhook_target", redactURL(cfg.WebhookURL),
		"webhook_method", cfg.WebhookMethod,
		"hmac_enabled", len(cfg.ResolvedHMACSecret) > 0,
		"compression_attribute", cfg.PayloadCompressionAttribute,
	)

	err = consumer.Run(ctx, cfg, fwd)
	if healthSrv != nil {
		healthSrv.SetReady(false)
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return nil
	}
	return err
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
