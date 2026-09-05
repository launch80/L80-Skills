package main

import (
	"os"

	"github.com/mgeatz/L80-Skills/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args))
}
