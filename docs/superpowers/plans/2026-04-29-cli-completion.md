# Go CLI 命令补全 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 补全 Go CLI 缺失的命令（config、check、search）和 --verbose 调试日志，使 Go 二进制成为功能完整的 CLI 工具，覆盖 README 中已文档化的所有命令。

**Architecture:** 用户输入 → cobra 解析子命令 → resolveClient 获取凭证 → joycode.Client 调用 API → 格式化输出。新增命令与现有命令模式一致：每个命令一个独立 .go 文件，在 init() 中注册到 rootCmd。--verbose 作为全局 flag 影响 Client 的日志输出行为。

**Tech Stack:** Go 1.23, spf13/cobra v1.10.2, mattn/go-sqlite3 v1.14.24, net/http 标准库

**Risks:**
- `check` 命令不依赖凭证（纯端口探测），需要绕过 resolveClient → 缓解：直接使用 http.Get 不调用 resolveClient
- `--verbose` 需要在 Client 层加日志，但 Client 是共享包 → 缓解：仅在 serve 命令中使用 log.Printf 做请求日志，不修改 Client 本身

---

### Task 1: Add `config` command — 显示当前解析后的配置信息

**Depends on:** None
**Files:**
- Create: `cmd/JoyCodeProxy/config.go`

- [ ] **Step 1: 创建 config.go — 显示凭证来源、默认模型、API 地址、服务安装状态**

```go
// cmd/JoyCodeProxy/config.go
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
		fmt.Printf("    Base URL:  %s\n", joycode.BaseURL)
		fmt.Printf("    Default Model: %s\n", joycode.DefaultModel)

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
```

- [ ] **Step 2: 验证 config 命令**
Run: `go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/ && ./joycode_proxy_bin config`
Expected:
  - Exit code: 0
  - Output contains: "JoyCode Proxy Configuration"
  - Output contains: "auto-detected"
  - Output contains: "Available Models"

- [ ] **Step 3: 提交**
Run: `git add cmd/JoyCodeProxy/config.go && git commit -m "feat(cli): add config command to display resolved configuration"`

---

### Task 2: Add `check` command — 检查代理服务是否运行

**Depends on:** None
**Files:**
- Create: `cmd/JoyCodeProxy/check.go`

- [ ] **Step 1: 创建 check.go — 对本地代理做健康检查，不依赖凭证**

```go
// cmd/JoyCodeProxy/check.go
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
	Use:   "check",
	Short: "Check if the proxy server is running",
	Long:  "Send a health check request to the proxy to verify it is running and responsive.",
	RunE: func(cmd *cobra.Command, args []string) error {
		url := fmt.Sprintf("http://localhost:%d/health", checkPort)
		client := &http.Client{Timeout: 5 * time.Second}

		resp, err := client.Get(url)
		if err != nil {
			fmt.Printf("  Status:   offline\n")
			fmt.Printf("  Address:  localhost:%d\n", checkPort)
			fmt.Printf("  Error:    %s\n", err)
			fmt.Println()
			fmt.Println("  Start the proxy with: JoyCodeProxy serve")
			fmt.Println("  Or install as service: JoyCodeProxy service install")
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
	checkCmd.Flags().IntVarP(&checkPort, "port", "p", 34891, "proxy port to check")
	rootCmd.AddCommand(checkCmd)
}
```

- [ ] **Step 2: 验证 check 命令**
Run: `go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/ && ./joycode_proxy_bin check`
Expected:
  - Exit code: 0
  - Output contains: "online" or "offline" (取决于 proxy 是否运行)
  - 输出格式化的状态信息

- [ ] **Step 3: 提交**
Run: `git add cmd/JoyCodeProxy/check.go && git commit -m "feat(cli): add check command for proxy health check"`

---

### Task 3: Add `search` command — CLI 网页搜索

**Depends on:** None
**Files:**
- Create: `cmd/JoyCodeProxy/search.go`

- [ ] **Step 1: 创建 search.go — 从 CLI 直接调用 JoyCode 网页搜索 API**

```go
// cmd/JoyCodeProxy/search.go
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
```

- [ ] **Step 2: 验证 search 命令编译**
Run: `go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/ && ./joycode_proxy_bin search --help`
Expected:
  - Exit code: 0
  - Output contains: "Perform a web search"
  - Output contains: "[query]"

- [ ] **Step 3: 提交**
Run: `git add cmd/JoyCodeProxy/search.go && git commit -m "feat(cli): add search command for web search"`

---

### Task 4: Add `--verbose` flag and request debug logging

**Depends on:** Task 1, Task 2, Task 3
**Files:**
- Modify: `cmd/JoyCodeProxy/root.go:1-70`（添加 --verbose flag）
- Modify: `cmd/JoyCodeProxy/serve.go:1-82`（添加请求日志中间件）

- [ ] **Step 1: 修改 root.go — 添加 --verbose 全局 flag**
文件: `cmd/JoyCodeProxy/root.go:1-70`（替换整个文件）

```go
// cmd/JoyCodeProxy/root.go
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
	Long:  "Convert JoyCode AI IDE APIs to OpenAI/Anthropic-compatible format for Codex, Claude Code, and other tools.",
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
```

- [ ] **Step 2: 修改 serve.go — 添加请求日志中间件**
文件: `cmd/JoyCodeProxy/serve.go:1-82`（替换整个文件）

```go
// cmd/JoyCodeProxy/serve.go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/anthropic"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/openai"
)

var (
	serveHost string
	servePort int
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the OpenAI-compatible proxy server",
	Long:  "Start an OpenAI-compatible API proxy that converts requests to JoyCode API format.",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := resolveClient()
		if err != nil {
			return err
		}
		srv := openai.NewServer(client)
		anth := anthropic.NewHandler(client)
		mux := http.NewServeMux()
		srv.RegisterRoutes(mux)
		anth.RegisterRoutes(mux)

		var handler http.Handler = mux
		if verbose {
			handler = loggingMiddleware(mux)
		}

		addr := fmt.Sprintf("%s:%d", serveHost, servePort)
		httpSrv := &http.Server{
			Addr:    addr,
			Handler: handler,
		}

		go func() {
			log.Printf("JoyCode Proxy running on http://%s", addr)
			fmt.Println("  Endpoints:")
			fmt.Println("    POST /v1/chat/completions  — Chat (OpenAI format)")
			fmt.Println("    POST /v1/messages          — Chat (Anthropic/Claude Code format)")
			fmt.Println("    POST /v1/web-search        — Web Search")
			fmt.Println("    POST /v1/rerank            — Rerank documents")
			fmt.Println("    GET  /v1/models            — Model list")
			fmt.Println("    GET  /health               — Health check")
			fmt.Println()
			fmt.Println("  Claude Code setup:")
			fmt.Printf("    export ANTHROPIC_BASE_URL=http://%s\n", addr)
			fmt.Println("    export ANTHROPIC_API_KEY=joycode")
			if verbose {
				fmt.Println()
				fmt.Println("  Verbose logging: enabled")
			}
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Server error: %v", err)
			}
		}()

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		log.Println("Shutting down server...")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
		log.Println("Server stopped")
		return nil
	},
}

func init() {
	serveCmd.Flags().StringVarP(&serveHost, "host", "H", "0.0.0.0", "bind host")
	serveCmd.Flags().IntVarP(&servePort, "port", "p", 34891, "bind port")
	rootCmd.AddCommand(serveCmd)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("-> %s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
		log.Printf("<- %s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}
```

- [ ] **Step 3: 验证全部命令编译通过**
Run: `go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/ && ./joycode_proxy_bin --help`
Expected:
  - Exit code: 0
  - Output contains: "config", "check", "search", "serve", "chat", "models", "whoami", "service", "version"
  - Output contains: "-v, --verbose"

- [ ] **Step 4: 验证 --verbose flag 正常工作**
Run: `./joycode_proxy_bin --verbose serve --skip-validation &  sleep 2 && kill %1`
Expected:
  - Output contains: "Verbose logging: enabled"
  - Output contains: "->" 请求日志（如果有请求进来）

- [ ] **Step 5: 提交**
Run: `git add cmd/JoyCodeProxy/root.go cmd/JoyCodeProxy/serve.go && git commit -m "feat(cli): add --verbose flag with request debug logging"`
