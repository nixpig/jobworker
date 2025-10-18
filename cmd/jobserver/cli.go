package main

import (
	"log/slog"
	"os"

	"github.com/nixpig/jobworker/internal/jobmanager"
	"github.com/spf13/cobra"
)

type serverConfig struct {
	port uint16

	serverCertPath string
	serverKeyPath  string
	caCertPath     string

	debug bool
}

func rootCmd() *cobra.Command {
	config := &serverConfig{}

	c := &cobra.Command{
		Use:     "jobserver",
		Short:   "gRPC server for executing arbitrary Linux commands on a remote host",
		Example: "jobserver --debug",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			var logLevel slog.Level

			if config.debug {
				logLevel = slog.LevelDebug
			} else {
				logLevel = slog.LevelInfo
			}

			logger := slog.New(
				slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
					Level: logLevel,
				}),
			)

			manager := jobmanager.NewManager()

			return newServer(manager, logger, config).Start()
		},
	}

	c.Flags().Uint16Var(&config.port, "port", 8443, "gRPC server port")
	c.Flags().BoolVar(&config.debug, "debug", false, "Enable debug logs")

	c.Flags().StringVar(
		&config.serverCertPath,
		"server-cert",
		"certs/server.crt",
		"Path to server certificate",
	)

	c.Flags().StringVar(
		&config.serverKeyPath,
		"server-key",
		"certs/server.key",
		"Path to server private key",
	)

	c.Flags().StringVar(
		&config.caCertPath,
		"ca-cert",
		"certs/ca.crt",
		"Path to CA certificate",
	)

	return c
}
