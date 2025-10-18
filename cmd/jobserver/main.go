package main

import (
	"fmt"
	"os"

	"github.com/nixpig/jobworker/internal/jobmanager"
)

const version = "0.0.1"

func main() {
	if err := rootCmd(jobmanager.NewManager()).Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}
}
