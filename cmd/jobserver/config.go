package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	// NOTE: The std lib flag package would be fine, but wanted consistent UX
	// between the client and server CLI without the overhead of cobra, so using
	// pflag package.
	"github.com/spf13/pflag"
)

type config struct {
	host       string
	port       string
	debug      bool
	certPath   string
	keyPath    string
	caCertPath string
}

func (c *config) validate() error {
	port, err := strconv.Atoi(c.port)
	if err != nil {
		return fmt.Errorf("port string to number: %w", err)
	}

	if port < 1 || port > 65535 {
		return errors.New("port must be in valid range")
	}

	if c.certPath == "" {
		return errors.New("cert-path cannot be empty")
	}

	if _, err := os.Stat(c.certPath); err != nil {
		return fmt.Errorf("failed to stat cert-path: %w", err)
	}

	if c.keyPath == "" {
		return errors.New("key-path cannot be empty")
	}

	if _, err := os.Stat(c.keyPath); err != nil {
		return fmt.Errorf("failed to stat key-path: %w", err)
	}

	if c.caCertPath == "" {
		return errors.New("ca-cert-path cannot be empty")
	}

	if _, err := os.Stat(c.caCertPath); err != nil {
		return fmt.Errorf("failed to stat ca-cert-path: %w", err)
	}

	return nil
}

func parseFlags() *config {
	cfg := &config{}

	pflag.StringVar(&cfg.host, "host", "localhost", "gRPC server host to bind")
	pflag.StringVar(&cfg.port, "port", "8443", "gRPC server port")
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
