// Package tlsconfig provides utilities for configuring mTLS.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// Config holds parameters needed to setup TLS for both clients and servers.
type Config struct {
	CertPath   string
	KeyPath    string
	CACertPath string
	// ServerName is the hostname to verify when connecting as a client. Only
	// used when Server is `false`.
	ServerName string
	// Server indicates whether to configure TLS for a server or client.
	Server bool
}

// SetupTLS creates a TLS configuration for mTLS authentication.
// When `config.Server = true`, requires and verifies client certs.
// When `config.Server = false`, uses CA to verify server cert.
//
// Cipher suites (not configurable in Go):
//   - TLS_AES_128_GCM_SHA256
//   - TLS_AES_256_GCM_SHA384
//   - TLS_CHACHA20_POLY1305_SHA256
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
		return nil, fmt.Errorf("failed to add CA certificate to pool: %w", err)
	}

	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: false,
		Certificates:       []tls.Certificate{cert},
	}

	if config.Server {
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		tlsConfig.ClientCAs = caCertPool
	} else {
		tlsConfig.RootCAs = caCertPool
		tlsConfig.ServerName = config.ServerName
	}

	return tlsConfig, nil
}
