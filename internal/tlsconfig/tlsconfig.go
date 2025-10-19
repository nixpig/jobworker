// Package tlsconfig provides utilities for configuring mTLS.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// Config holds paramters needed to setup TLS for both clients and servers.
type Config struct {
	CertPath   string
	KeyPath    string
	CACertPath string
	ServerAddr string
	Server     bool
}

// TODO: Add unit tests for production solution. For this prototype, I think
// it's sufficiently exercised by server integration tests.

// SetupTLS creates a TLS configuration for mTLS authentication.
// When `config.server = true`, requires and verifies client certs.
// When `config.server = false`, uses CA to verify server cert.
func SetupTLS(config *Config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(config.CertPath, config.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate: %w", err)
	}

	caCert, err := os.ReadFile(config.CACertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: false,
		ServerName:         config.ServerAddr,
		Certificates:       []tls.Certificate{cert},
	}

	if config.Server {
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		tlsConfig.ClientCAs = caCertPool
	} else {
		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig, nil
}
