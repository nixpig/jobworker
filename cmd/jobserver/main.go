// Command jobserver implements a gRPC server for handling requests to execute
// arbitrary Linux processes and stream their output using the jobmanager
// library.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/nixpig/jobworker/internal/jobmanager"
)

// TODO: Inject version at build time.
const version = "0.0.1"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg := parseFlags()

	if err := cfg.validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	logger := newLogger(cfg.debug)

	manager, err := jobmanager.NewManagerWithDefaults()
	if err != nil {
		logger.Error("failed to create manager with cgroups", "err", err)
		os.Exit(1)
	}

	server := newServer(manager, logger, cfg)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.port))
	if err != nil {
		logger.Error("failed to create listener", "port", cfg.port, "err", err)
		return fmt.Errorf("create TCP listener: %w", err)
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting server", "port", cfg.port, "version", version)
		errCh <- server.start(listener)
	}()

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGTERM,
		os.Interrupt,
	)
	defer cancel()

	select {
	case err := <-errCh:
		if err != nil {
			logger.Error("server stopped with error", "err", err)
			return fmt.Errorf("server stopped with error: %w", err)
		}
	case <-ctx.Done():
		logger.Info("shutting down server")
		server.shutdown()
		_ = <-errCh
		manager.Shutdown()
	}

	logger.Info("server shutdown cleanly")
	return nil
}

func newLogger(debug bool) *slog.Logger {
	var level slog.Level

	if debug {
		level = slog.LevelDebug
	} else {
		level = slog.LevelInfo
	}

	// NOTE: Using a text handler, since this is running locally. In production,
	// would use JSON so it can be easily hooked up to existing solution.
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})

	return slog.New(handler)
}
