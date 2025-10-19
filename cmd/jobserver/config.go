package main

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"
)

type config struct {
	port       uint16
	debug      bool
	certPath   string
	keyPath    string
	caCertPath string
}

func (c *config) validate() error {
	if c.port == 0 {
		return fmt.Errorf("port must be in valid range")
	}

	if c.certPath == "" {
		return fmt.Errorf("cert-path cannot be empty")
	}

	if _, err := os.Stat(c.certPath); err != nil {
		return fmt.Errorf("failed to stat cert-path: %w", err)
	}

	if c.keyPath == "" {
		return fmt.Errorf("key-path cannot be empty")
	}

	if _, err := os.Stat(c.keyPath); err != nil {
		return fmt.Errorf("failed to stat key-path: %w", err)
	}

	if c.caCertPath == "" {
		return fmt.Errorf("ca-cert-path cannot be empty")
	}

	if _, err := os.Stat(c.caCertPath); err != nil {
		return fmt.Errorf("failed to stat ca-cert-path: %w", err)
	}

	return nil
}

func parseFlags() *config {
	cfg := &config{}

	pflag.Uint16Var(&cfg.port, "port", 8443, "gRPC server port")
	pflag.BoolVar(&cfg.debug, "debug", false, "Enable debug logs")

	pflag.StringVar(
		&cfg.certPath,
		"cert-path",
		"certs/server.crt",
		"Path to server TLS certificate",
	)

	pflag.StringVar(
		&cfg.keyPath,
		"key-path",
		"certs/server.key",
		"Path to server TLS private key",
	)

	pflag.StringVar(
		&cfg.caCertPath,
		"ca-cert-path",
		"certs/ca.crt",
		"Path to CA certificate for mTLS",
	)

	pflag.Parse()

	return cfg
}
