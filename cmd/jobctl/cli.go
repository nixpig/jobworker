package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"text/tabwriter"

	api "github.com/nixpig/jobworker/api/v1"
	"github.com/nixpig/jobworker/internal/tlsconfig"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// TODO: Inject version at build time.
const version = "0.0.1"

// TODO: Consider introducing config management like Viper.
type config struct {
	serverHostname string
	serverPort     string
	caCertPath     string
	certPath       string
	keyPath        string
}

type cli struct {
	client api.JobServiceClient
	conn   *grpc.ClientConn
}

func newCLI() *cli {
	return &cli{}
}

func (c *cli) rootCmd() *cobra.Command {
	cfg := &config{}

	command := &cobra.Command{
		Use:          "jobctl",
		Short:        "CLI for interacting with jobworker server",
		Version:      version,
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			tlsConfig, err := tlsconfig.SetupTLS(&tlsconfig.Config{
				CertPath:   cfg.certPath,
				KeyPath:    cfg.keyPath,
				CACertPath: cfg.caCertPath,
				ServerName: cfg.serverHostname,
			})
			if err != nil {
				return err
			}

			c.conn, err = grpc.NewClient(
				net.JoinHostPort(
					cfg.serverHostname,
					cfg.serverPort,
				),
				grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
			)
			if err != nil {
				return err
			}

			c.client = api.NewJobServiceClient(c.conn)

			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			if c.conn == nil {
				return nil
			}

			// Connection needs to remain open for duration of any child commands.
			return c.conn.Close()
		},
	}

	command.AddCommand(
		c.startCmd(),
		c.stopCmd(),
		c.statusCmd(),
		c.streamCmd(),
	)

	command.CompletionOptions.HiddenDefaultCmd = true

	command.PersistentFlags().StringVar(
		&cfg.serverHostname,
		"server-hostname",
		"localhost",
		"Server hostname",
	)

	command.PersistentFlags().StringVar(
		&cfg.serverPort,
		"server-port",
		"8443",
		"Server port",
	)

	command.PersistentFlags().StringVar(
		&cfg.certPath,
		"cert-path",
		"certs/client-operator.crt",
		"Path to client TLS certificate",
	)

	command.PersistentFlags().StringVar(
		&cfg.keyPath,
		"key-path",
		"certs/client-operator.key",
		"Path to client TLS private key",
	)

	command.PersistentFlags().StringVar(
		&cfg.caCertPath,
		"ca-cert-path",
		"certs/ca.crt",
		"Path to CA certificate for mTLS",
	)

	return command
}

func (c *cli) startCmd() *cobra.Command {
	command := &cobra.Command{
		Use:     "start [flags] JOB_PROGRAM [JOB_ARGS]",
		Short:   "Start a new job",
		Example: "  jobctl start tail -f server.log",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := c.client.RunJob(
				cmd.Context(),
				&api.RunJobRequest{
					Program: args[0],
					Args:    args[1:],
				},
			)
			if err != nil {
				return mapError(err)
			}

			cmd.OutOrStdout().Write([]byte(resp.Id + "\n"))

			return nil
		},
	}

	// Stop parsing args after first position so that flags passed to the program
	// to run are not interpreted by the jobctl CLI and are passed as-is,
	// e.g. `-f` is an argument to `tail` _not_ to `jobctl start`:
	//	`jobctl start tail -f server.log`
	command.Flags().SetInterspersed(false)

	return command
}

func (c *cli) statusCmd() *cobra.Command {
	command := &cobra.Command{
		Use:     "status [flags] JOB_ID",
		Short:   "Query status of job",
		Example: "  jobctl status 9302033c-f8f7-4b6e-9363-a7aa201cce1b",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := c.client.QueryJob(
				cmd.Context(),
				&api.QueryJobRequest{Id: args[0]},
			)
			if err != nil {
				return mapError(err)
			}

			// TODO: Only output headers if TTY. Or could add a flag like --plain or
			// --skip-headers to hide headers.
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

			fmt.Fprintf(w, "STATE\tEXIT CODE\tSIGNAL\tINTERRUPTED\t\n")
			fmt.Fprintf(
				w,
				"%s\t%d\t%s\t%t\t\n",
				mapState(resp.State),
				resp.ExitCode,
				resp.Signal,
				resp.Interrupted,
			)

			w.Flush()

			return nil
		},
	}

	return command
}

func (c *cli) stopCmd() *cobra.Command {
	command := &cobra.Command{
		Use:     "stop [flags] JOB_ID",
		Short:   "Stop a running job",
		Example: "  jobctl stop 9302033c-f8f7-4b6e-9363-a7aa201cce1b",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := c.client.StopJob(
				cmd.Context(),
				&api.StopJobRequest{Id: args[0]},
			); err != nil {
				return mapError(err)
			}

			return nil
		},
	}

	return command
}

func (c *cli) streamCmd() *cobra.Command {
	command := &cobra.Command{
		Use:     "stream [flags] JOB_ID",
		Short:   "Stream job output",
		Example: "  jobctl stream 9302033c-f8f7-4b6e-9363-a7aa201cce1b",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stream, err := c.client.StreamJobOutput(
				cmd.Context(),
				&api.StreamJobOutputRequest{Id: args[0]},
			)
			if err != nil {
				return mapError(err)
			}

			for {
				resp, err := stream.Recv()
				if err != nil {
					if err == io.EOF {
						break
					}

					if status.Code(err) == codes.Canceled {
						break
					}

					return mapError(err)
				}

				cmd.OutOrStdout().Write(resp.Output)
			}

			return nil
		},
	}

	return command
}

// mapError translates gRPC errors to human-readable messages.
func mapError(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return err
	}

	switch st.Code() {
	case codes.NotFound:
		return errors.New("not found")
	case codes.PermissionDenied:
		return errors.New("permission denied")
	case codes.Unauthenticated:
		return errors.New("not authenticated")
	case codes.InvalidArgument:
		return fmt.Errorf("%s", st.Message())
	case codes.Unavailable:
		return errors.New("server unavailable")
	default:
		return fmt.Errorf("%s", st.Message())
	}
}

// mapState translates gRPC JobState enum values to human-readable strings.
func mapState(state api.JobState) string {
	switch state {
	case api.JobState_JOB_STATE_UNSPECIFIED:
		return "Unspecified"
	case api.JobState_JOB_STATE_CREATED:
		return "Created"
	case api.JobState_JOB_STATE_STARTING:
		return "Starting"
	case api.JobState_JOB_STATE_STARTED:
		return "Started"
	case api.JobState_JOB_STATE_STOPPING:
		return "Stopping"
	case api.JobState_JOB_STATE_STOPPED:
		return "Stopped"
	case api.JobState_JOB_STATE_FAILED:
		return "Failed"
	default:
		return fmt.Sprintf("Unknown(%d)", state)
	}
}
