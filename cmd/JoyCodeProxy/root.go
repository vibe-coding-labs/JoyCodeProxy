package main

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/auth"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

var (
	ptKey          string
	userID         string
	skipValidation bool
	verbose        bool
)

var rootCmd = &cobra.Command{
	Use:   "JoyCodeProxy",
	Short: "JoyCode OpenAI-Compatible API Proxy",
	Long:  "Convert JoyCode AI IDE APIs to OpenAI-compatible format for Codex and other tools.",
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&ptKey, "ptkey", "k", "", "JoyCode ptKey (auto-detected if empty)")
	rootCmd.PersistentFlags().StringVarP(&userID, "userid", "u", "", "JoyCode userID (auto-detected if empty)")
	rootCmd.PersistentFlags().BoolVar(&skipValidation, "skip-validation", false, "skip credential validation on startup")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")
}

func resolveClient() (*joycode.Client, error) {
	var creds *auth.Credentials
	var source string

	if ptKey != "" && userID != "" {
		creds = &auth.Credentials{PtKey: ptKey, UserID: userID}
		source = "flags"
	} else {
		detected, err := auth.LoadFromSystem()
		if err != nil {
			return nil, fmt.Errorf("cannot auto-detect credentials: %w\n  Please provide --ptkey and --userid flags, or log in to JoyCode first", err)
		}
		creds = detected
		source = "auto-detected"

		// Partial override: flag value takes precedence
		if ptKey != "" {
			creds.PtKey = ptKey
			source = "flags+auto-detected"
		}
		if userID != "" {
			creds.UserID = userID
			source = "flags+auto-detected"
		}
	}

	log.Printf("Credentials source: %s (userId=%s)", source, creds.UserID)
	client := joycode.NewClient(creds.PtKey, creds.UserID)

	if skipValidation {
		log.Printf("Credential validation skipped (--skip-validation)")
		return client, nil
	}

	log.Printf("Validating credentials...")
	if err := client.Validate(); err != nil {
		return nil, fmt.Errorf("%w\n  Your credentials may have expired. Try re-logging into JoyCode or provide fresh --ptkey and --userid", err)
	}
	log.Printf("Credentials validated successfully")
	return client, nil
}
