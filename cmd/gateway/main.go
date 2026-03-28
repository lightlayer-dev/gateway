package main

import (
	"os"

	"github.com/lightlayer-dev/gateway/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
