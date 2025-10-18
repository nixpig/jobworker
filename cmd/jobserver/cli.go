package main

import (
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

func rootCmd(manager *jobmanager.Manager) *cobra.Command {
	config := &serverConfig{}

	c := &cobra.Command{
		Use:     "jobserver",
		Short:   "gRPC server for executing arbitrary Linux commands on a remote host",
		Example: "jobserver --debug",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServer(config, manager)
		},
	}

	c.Flags().Uint16Var(&config.port, "port", 8443, "gRPC server port")
	c.Flags().BoolVar(&config.debug, "debug", false, "Enable debug logs")

	c.Flags().
		StringVar(&config.serverCertPath, "server-cert", "certs/server.crt", "Path to server certificate")

	c.Flags().
		StringVar(&config.serverKeyPath, "server-key", "certs/server.key", "Path to server private key")

	c.Flags().
		StringVar(&config.caCertPath, "ca-cert", "certs/ca.crt", "Path to CA certificate")

	return c
}
