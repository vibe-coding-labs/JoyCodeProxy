package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Perform a web search",
	Long:  "Search the web using JoyCode's built-in search API.",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := resolveClient()
		if err != nil {
			return err
		}
		results, err := client.WebSearch(args[0])
		if err != nil {
			return err
		}
		if len(results) == 0 {
			fmt.Println("No results found.")
			return nil
		}
		fmt.Printf("Search results for: %s\n\n", args[0])
		for i, r := range results {
			item, ok := r.(map[string]interface{})
			if !ok {
				continue
			}
			title, _ := item["title"].(string)
			url, _ := item["url"].(string)
			snippet, _ := item["snippet"].(string)
			fmt.Printf("  %d. %s\n", i+1, title)
			if url != "" {
				fmt.Printf("     %s\n", url)
			}
			if snippet != "" {
				fmt.Printf("     %s\n", snippet)
			}
			fmt.Println()
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(searchCmd)
}
