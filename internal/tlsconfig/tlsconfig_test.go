package tlsconfig_test

import (
	"crypto/tls"
	"testing"

	"github.com/nixpig/jobworker/internal/tlsconfig"
)

// TODO: For production-ready solution, add scenarios to cover all error paths.
func TestSetupTLS(t *testing.T) {
	t.Parallel()

	t.Run("Test server TLS config", func(t *testing.T) {
		t.Parallel()

		tlsConfig, err := tlsconfig.SetupTLS(&tlsconfig.Config{
			CertPath:   "../../certs/server.crt",
			KeyPath:    "../../certs/server.key",
			CACertPath: "../../certs/ca.crt",
			Server:     true,
		})
		if err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
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

		if len(tlsConfig.Certificates) == 0 {
			t.Errorf("expected certificates to be present")
		}
	})

	t.Run("Test client TLS config", func(t *testing.T) {
		t.Parallel()

		tlsConfig, err := tlsconfig.SetupTLS(&tlsconfig.Config{
			CertPath:   "../../certs/client-operator.crt",
			KeyPath:    "../../certs/client-operator.key",
			CACertPath: "../../certs/ca.crt",
			Server:     false,
			ServerName: "localhost",
		})
		if err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
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

		if len(tlsConfig.Certificates) == 0 {
			t.Errorf("expected certificates to be present")
		}
	})

}
