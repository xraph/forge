//go:build ignore

// gen_ports regenerates the ported helper files from the TypeScript generator.
// Run it through go generate (see naming.go) after the TypeScript helpers change.
package main

import (
	"fmt"
	"os"

	"github.com/xraph/forge/internal/client/generators/dart/portgen"
)

func main() {
	if err := portgen.Write("../typescript", "."); err != nil {
		fmt.Fprintln(os.Stderr, "gen_ports:", err)
		os.Exit(1)
	}
}
