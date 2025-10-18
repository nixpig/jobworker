package main

import (
	"fmt"
	"os"
)

// TODO: Inject version at build time
const version = "0.0.1"

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}
}
