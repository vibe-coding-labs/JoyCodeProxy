package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

var checkPort int

var checkCmd = &cobra.Command{
	Use:     "check",
	Short:   "检查代理服务是否运行",
	Long:    "向本地代理发送健康检查请求，验证服务是否正常运行。",
	GroupID: "core",
	Example: `  # 检查默认端口
  joycode-proxy check

  # 检查指定端口
  joycode-proxy check -p 8080`,
	RunE: func(cmd *cobra.Command, args []string) error {
		url := fmt.Sprintf("http://localhost:%d/health", checkPort)
		client := &http.Client{Timeout: 5 * time.Second}

		resp, err := client.Get(url)
		if err != nil {
			fmt.Printf("  Status:   offline\n")
			fmt.Printf("  Address:  localhost:%d\n", checkPort)
			fmt.Printf("  Error:    %s\n", err)
			fmt.Println()
			fmt.Println("  Start the proxy with: joycode-proxy serve")
			fmt.Println("  Or install as service: joycode-proxy service install")
			return nil
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			fmt.Printf("  Status:   online (unexpected response)")
			return nil
		}

		status, _ := result["status"].(string)
		service, _ := result["service"].(string)

		if status == "ok" {
			fmt.Printf("  Status:   online\n")
		} else {
			fmt.Printf("  Status:   %s\n", status)
		}
		fmt.Printf("  Address:  localhost:%d\n", checkPort)
		fmt.Printf("  Service:  %s\n", service)
		if endpoints, ok := result["endpoints"].([]interface{}); ok {
			fmt.Printf("  Endpoints: %d registered\n", len(endpoints))
			for _, ep := range endpoints {
				fmt.Printf("    - %s\n", ep)
			}
		}
		return nil
	},
}

func init() {
	checkCmd.Flags().IntVarP(&checkPort, "port", "p", 34891, "检查端口")
	rootCmd.AddCommand(checkCmd)
}
