// Command jobctl is a CLI client for interacting with a job server.
package main

import "os"

func main() {
	if err := newCLI().rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
