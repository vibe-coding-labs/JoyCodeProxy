package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/auth"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Display current configuration",
	Long:  "Show resolved credentials, default settings, and service status.",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("JoyCode Proxy Configuration")
		fmt.Println("============================")

		// Credentials
		fmt.Println()
		fmt.Println("  Credentials:")
		if ptKey != "" && userID != "" {
			fmt.Printf("    Source:    flags\n")
			fmt.Printf("    UserID:    %s\n", userID)
			fmt.Printf("    PtKey:     %s...%s\n", ptKey[:8], ptKey[len(ptKey)-4:])
		} else {
			creds, err := auth.LoadFromSystem()
			if err != nil {
				fmt.Printf("    Source:    not available (%s)\n", err)
			} else {
				fmt.Printf("    Source:    auto-detected\n")
				fmt.Printf("    UserID:    %s\n", creds.UserID)
				fmt.Printf("    PtKey:     %s...%s\n", creds.PtKey[:8], creds.PtKey[len(creds.PtKey)-4:])
			}
		}

		// API
		fmt.Println()
		fmt.Println("  API:")
		fmt.Printf("    Base URL:       %s\n", joycode.BaseURL)
		fmt.Printf("    Default Model:  %s\n", joycode.DefaultModel)

		// Server
		fmt.Println()
		fmt.Println("  Server:")
		fmt.Printf("    Default Host:  %s\n", "0.0.0.0")
		fmt.Printf("    Default Port:  %d\n", 34891)

		// Service
		fmt.Println()
		fmt.Println("  Service:")
		home, _ := os.UserHomeDir()
		plistPath := filepath.Join(home, "Library", "LaunchAgents", plistName)
		if _, err := os.Stat(plistPath); os.IsNotExist(err) {
			fmt.Println("    Installed: no")
		} else {
			fmt.Println("    Installed: yes")
			fmt.Printf("    Plist:     %s\n", plistPath)
		}

		// Models
		fmt.Println()
		fmt.Println("  Available Models:")
		for _, m := range joycode.Models {
			suffix := ""
			if m == joycode.DefaultModel {
				suffix = " (default)"
			}
			fmt.Printf("    - %s%s\n", m, suffix)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
}
