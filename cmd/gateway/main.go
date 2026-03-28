package main

import (
	"os"

	gateway "github.com/lightlayer-dev/gateway"
	"github.com/lightlayer-dev/gateway/internal/cli"
)

func main() {
	cli.UIAssets = gateway.UIAssets
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
