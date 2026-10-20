// Command gonest scaffolds Gonest projects and components.
//
// Owner: agent A4.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "gonest",
		Short: "Scaffold Gonest projects and components",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
