// Command jobctl is a CLI client for interacting with a job server.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGTERM,
		os.Interrupt,
	)
	defer cancel()

	if err := newCLI().rootCmd().ExecuteContext(ctx); err != nil {
		return err
	}

	return nil
}
