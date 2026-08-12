package main

import (
	"fmt"
	"os"

	"github.com/AxLabs/simple-agent-wallet/internal/cli"
)

// set by goreleaser / Makefile
var version = "dev"

func main() {
	cli.Version = version
	if err := cli.NewRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
