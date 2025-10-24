package tlsconfig_test

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	"github.com/nixpig/jobworker/certs"
	"github.com/nixpig/jobworker/internal/tlsconfig"
)

// TODO: For production-ready solution, add scenarios to cover all error paths.
func TestSetupTLS(t *testing.T) {
	t.Parallel()

	certDir := t.TempDir()

	certFiles := []string{
		"ca.crt",
		"server.crt",
		"server.key",
		"client-operator.crt",
		"client-operator.key",
	}

	for _, filename := range certFiles {
		data, err := certs.FS.ReadFile(filename)
		if err != nil {
			t.Fatalf("read cert %s: %v", filename, err)
		}

		path := filepath.Join(certDir, filename)
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("save cert %s: %v", filename, err)
		}
	}

	caCertPath := filepath.Join(certDir, "ca.crt")
	serverCertPath := filepath.Join(certDir, "server.crt")
	serverKeyPath := filepath.Join(certDir, "server.key")
	operatorCertPath := filepath.Join(certDir, "client-operator.crt")
	operatorKeyPath := filepath.Join(certDir, "client-operator.key")

	t.Run("Test server TLS config", func(t *testing.T) {
		t.Parallel()

		tlsConfig, err := tlsconfig.SetupTLS(&tlsconfig.Config{
			CertPath:   serverCertPath,
			KeyPath:    serverKeyPath,
			CACertPath: caCertPath,
			Server:     true,
		})
		if err != nil {
			t.Errorf("expected TLS setup not to return error: got '%v'", err)
		}

		if tlsConfig.MinVersion != tls.VersionTLS13 {
			t.Errorf(
				"expected min TLS version: got '%v', want '%v'",
				tlsConfig.MinVersion,
				tls.VersionTLS13,
			)
		}

		if tlsConfig.ClientAuth != tls.RequireAndVerifyClientCert {
			t.Errorf(
				"expected client auth: got '%v', want '%v'",
				tlsConfig.ClientAuth,
				tls.RequireAndVerifyClientCert,
			)
		}

		if tlsConfig.ClientCAs == nil {
			t.Errorf("expected client CAs to be set")
		}

		if tlsConfig.InsecureSkipVerify != false {
			t.Errorf(
				"expected insecure skip verify: got '%t', want 'false'",
				tlsConfig.InsecureSkipVerify,
			)
		}
	})

	t.Run("Test client TLS config", func(t *testing.T) {
		t.Parallel()

		tlsConfig, err := tlsconfig.SetupTLS(&tlsconfig.Config{
			CertPath:   operatorCertPath,
			KeyPath:    operatorKeyPath,
			CACertPath: caCertPath,
			Server:     false,
			ServerName: "localhost",
		})
		if err != nil {
			t.Errorf("expected TLS setup not to return error: got '%v'", err)
		}

		if tlsConfig.MinVersion != tls.VersionTLS13 {
			t.Errorf(
				"expected min TLS version: got '%v', want '%v'",
				tlsConfig.MinVersion,
				tls.VersionTLS13,
			)
		}

		if tlsConfig.ServerName != "localhost" {
			t.Errorf(
				"expected server name: got '%s', want 'localhost'",
				tlsConfig.ServerName,
			)
		}

		if tlsConfig.InsecureSkipVerify != false {
			t.Errorf(
				"expected insecure skip verify: got '%t', want 'false'",
				tlsConfig.InsecureSkipVerify,
			)
		}
	})
}
