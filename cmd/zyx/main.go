// Command zyx is the zyx compiler.
package main

import (
	"os"

	"zyx/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
