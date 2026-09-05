package main

import (
	"os"

	"github.com/launch80/L80-Skills/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args))
}
