// Command gonest scaffolds Gonest projects and components.
//
// Usage:
//
//	gonest new <name> [--module path] [--dir path] [--gonest-replace path] [--minimal]
//	gonest generate|g <component> <name> [--path dir] [--flat] [--dry-run] [--force]
//	gonest info
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr, "."); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
