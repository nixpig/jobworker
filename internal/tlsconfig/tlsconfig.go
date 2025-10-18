package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

func BaseTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: false,
		Certificates:       []tls.Certificate{},
	}
}

func LoadCertAndCA(
	certPath, keyPath, caCertPath string,
) (tls.Certificate, *x509.CertPool, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf(
			"failed to load certificate: %w",
			err,
		)
	}

	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf(
			"failed to read CA certificate: %w",
			err,
		)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return tls.Certificate{}, nil, fmt.Errorf(
			"failed to parse CA certificate: %w",
			err,
		)
	}

	return cert, caCertPool, nil
}
