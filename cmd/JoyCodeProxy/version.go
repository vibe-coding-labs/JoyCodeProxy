package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

var Version = "0.3.0"

var versionCmd = &cobra.Command{
	Use:     "version",
	Short:   "显示版本信息",
	GroupID: "query",
	Example: `  joycode-proxy version`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("JoyCode Proxy %s\n", Version)
		fmt.Printf("  JoyCode API: %s\n", joycode.ClientVersion)
		fmt.Printf("  Go:          %s\n", runtime.Version())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
