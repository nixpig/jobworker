package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nixpig/jobworker/internal/jobmanager"
	"github.com/spf13/pflag"
)

// TODO: Inject version at build time.
const version = "0.0.1"

type config struct {
	port           uint16
	debug          bool
	serverCertPath string
	serverKeyPath  string
	caCertPath     string
}

func main() {
	cfg := parseFlags()

	logger := newLogger(cfg.debug)

	manager := jobmanager.NewManager()
	defer manager.Shutdown()

	server := newServer(manager, logger, cfg)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting server", "port", cfg.port, "version", version)
		errCh <- server.start()
	}()

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGKILL,
		syscall.SIGTERM,
		os.Interrupt,
	)
	defer cancel()

	select {
	case err := <-errCh:
		if err != nil {
			logger.Error("server stopped with error", "err", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("shutting down server")
		server.shutdown()
	}

	logger.Info("done")
}

func parseFlags() *config {
	cfg := &config{}

	pflag.Uint16Var(&cfg.port, "port", 8443, "gRPC server port")
	pflag.BoolVar(&cfg.debug, "debug", false, "Enable debug logs")

	pflag.StringVar(
		&cfg.serverCertPath,
		"server-cert",
		"certs/server.crt",
		"Path to server certificate",
	)

	pflag.StringVar(
		&cfg.serverKeyPath,
		"server-key",
		"certs/server.key",
		"Path to server private key",
	)

	pflag.StringVar(
		&cfg.caCertPath,
		"ca-cert",
		"certs/ca.crt",
		"Path to CA certificate",
	)

	pflag.Parse()

	return cfg
}

func newLogger(debug bool) *slog.Logger {
	var level slog.Level

	if debug {
		level = slog.LevelDebug
	} else {
		level = slog.LevelInfo
	}

	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))
}
