package main

import (
	"os"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd.AddGroup(
		&cobra.Group{ID: "core", Title: "Core Commands:"},
		&cobra.Group{ID: "service", Title: "Service Management:"},
		&cobra.Group{ID: "query", Title: "Query & Info:"},
	)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
