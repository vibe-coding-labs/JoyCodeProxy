# Cross-Platform Service & Daemon Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 为 JoyCode Proxy CLI 添加两种后台运行能力：(1) 系统服务安装（macOS launchd + Linux systemd），开机自启；(2) 通用守护进程模式，崩溃自动重启，无需 root 权限。

**Architecture:** 用户执行 `joycode-proxy service install` → 检测平台 → 生成对应配置文件（macOS: plist → ~/Library/LaunchAgents，Linux: systemd unit → ~/.config/systemd/user）→ 注册并启动。守护进程模式：`joycode-proxy daemon start` → 主进程 fork 子进程运行 `serve` → 主进程成为 supervisor 监控子进程 → 子进程崩溃时自动重启（指数退避）→ PID 文件锁保证唯一实例。

**Tech Stack:** Go 1.23, Cobra CLI, os/exec, os/signal, build tags (darwin/linux)

**Risks:**
- Task 1 重构 service.go 不能破坏现有 macOS plist 安装 → 缓解：保持 plist 生成逻辑不变，只抽离平台检测
- Task 2 Daemon supervisor 的 re-exec 模式需要传递状态 → 缓解：通过环境变量 `_JOYCODE_DAEMON_CHILD=1` 标识子进程
- Task 3 Linux systemd 需要不同的路径和命令 → 缓解：使用 build tags 编译平台特定代码

---

### Task 1: 重构 Service 命令 — 提取平台检测 + 添加 Linux systemd 支持

**Depends on:** None
**Files:**
- Create: `cmd/JoyCodeProxy/service_linux.go`
- Modify: `cmd/JoyCodeProxy/service.go:35-125`（install 命令改为平台分发）
- Modify: `cmd/JoyCodeProxy/service.go:127-149`（uninstall 命令改为平台分发）

- [ ] **Step 1: 创建 service_linux.go — Linux systemd user service 安装/卸载**

```go
// cmd/JoyCodeProxy/service_linux.go
//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

func installService(port int) error {
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine binary path: %w", err)
	}
	binPath, err = filepath.Abs(binPath)
	if err != nil {
		return fmt.Errorf("cannot resolve binary path: %w", err)
	}

	configDir := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("cannot create systemd directory: %w", err)
	}

	unitData := struct {
		Description string
		BinaryPath  string
		Port        int
	}{
		Description: "JoyCode API Proxy",
		BinaryPath:  binPath,
		Port:        port,
	}

	unitTmpl := `[Unit]
Description={{.Description}}
After=network.target

[Service]
Type=simple
ExecStart={{.BinaryPath}} serve --port {{.Port}}
Restart=always
RestartSec=5
Environment=HOME=/root

[Install]
WantedBy=default.target
`

	unitPath := filepath.Join(configDir, serviceLabel+".service")
	f, err := os.Create(unitPath)
	if err != nil {
		return fmt.Errorf("cannot create unit file: %w", err)
	}
	defer f.Close()

	t, err := template.New("unit").Parse(unitTmpl)
	if err != nil {
		return err
	}
	if err := t.Execute(f, unitData); err != nil {
		return err
	}

	cmds := [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", serviceLabel + ".service"},
		{"systemctl", "--user", "start", serviceLabel + ".service"},
	}
	for _, args := range cmds {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s failed: %s: %w", args[0], string(out), err)
		}
	}

	fmt.Printf("Service installed and started.\n")
	fmt.Printf("  Unit:   %s\n", unitPath)
	fmt.Printf("  Port:   %d\n", port)
	return nil
}

func uninstallService() error {
	exec.Command("systemctl", "--user", "stop", serviceLabel+".service").Run()
	exec.Command("systemctl", "--user", "disable", serviceLabel+".service").Run()

	configDir := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user")
	unitPath := filepath.Join(configDir, serviceLabel+".service")

	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		fmt.Println("Service not installed (unit file not found).")
		return nil
	}

	if err := os.Remove(unitPath); err != nil {
		return fmt.Errorf("cannot remove unit file: %w", err)
	}
	exec.Command("systemctl", "--user", "daemon-reload").Run()

	fmt.Println("Service stopped and removed.")
	return nil
}

func serviceStatus() error {
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user")
	unitPath := filepath.Join(configDir, serviceLabel+".service")
	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		fmt.Println("Service not installed.")
		return nil
	}

	out, err := exec.Command("systemctl", "--user", "status", serviceLabel+".service").CombinedOutput()
	if err != nil {
		fmt.Printf("Service status:\n%s\n", string(out))
		return nil
	}
	fmt.Printf("Service status:\n%s\n", string(out))
	return nil
}
```

- [ ] **Step 2: 创建 service_darwin.go — macOS 平台安装/卸载实现**

将当前 `service.go` 中的平台特定逻辑提取到 `service_darwin.go`。

```go
// cmd/JoyCodeProxy/service_darwin.go
//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

func installService(port int) error {
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine binary path: %w", err)
	}
	binPath, err = filepath.Abs(binPath)
	if err != nil {
		return fmt.Errorf("cannot resolve binary path: %w", err)
	}

	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, logDir)
	if err := os.MkdirAll(logPath, 0755); err != nil {
		return fmt.Errorf("cannot create log directory: %w", err)
	}

	plistData := struct {
		Label      string
		BinaryPath string
		Port       int
		HomeDir    string
		StdoutLog  string
		StderrLog  string
	}{
		Label:      serviceLabel,
		BinaryPath: binPath,
		Port:       port,
		HomeDir:    home,
		StdoutLog:  filepath.Join(logPath, "stdout.log"),
		StderrLog:  filepath.Join(logPath, "stderr.log"),
	}

	tmpl := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>serve</string>
        <string>--port</string>
        <string>{{.Port}}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ThrottleInterval</key>
    <integer>10</integer>
    <key>StandardOutPath</key>
    <string>{{.StdoutLog}}</string>
    <key>StandardErrorPath</key>
    <string>{{.StderrLog}}</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>{{.HomeDir}}</string>
    </dict>
</dict>
</plist>`

	plistPath := filepath.Join(home, "Library", "LaunchAgents", plistName)
	f, err := os.Create(plistPath)
	if err != nil {
		return fmt.Errorf("cannot create plist: %w", err)
	}
	defer f.Close()

	t, err := template.New("plist").Parse(tmpl)
	if err != nil {
		return err
	}
	if err := t.Execute(f, plistData); err != nil {
		return err
	}

	out, err := exec.Command("launchctl", "load", plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl load failed: %s: %w", string(out), err)
	}

	fmt.Printf("Service installed and started.\n")
	fmt.Printf("  Label:   %s\n", serviceLabel)
	fmt.Printf("  Plist:   %s\n", plistPath)
	fmt.Printf("  Port:    %d\n", port)
	fmt.Printf("  Logs:    %s/\n", logPath)
	return nil
}

func uninstallService() error {
	home, _ := os.UserHomeDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", plistName)

	exec.Command("launchctl", "unload", plistPath).Run()

	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		fmt.Println("Service not installed (plist not found).")
		return nil
	}

	if err := os.Remove(plistPath); err != nil {
		return fmt.Errorf("cannot remove plist: %w", err)
	}

	fmt.Println("Service stopped and removed.")
	return nil
}

func serviceStatus() error {
	home, _ := os.UserHomeDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", plistName)
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		fmt.Println("Service not installed.")
		return nil
	}

	out, err := exec.Command("launchctl", "list").Output()
	if err != nil {
		return fmt.Errorf("launchctl list failed: %w", err)
	}

	found := false
	lines := splitLines(string(out))
	for _, line := range lines {
		if containsStr(line, serviceLabel) {
			fmt.Printf("Service status: %s\n", line)
			found = true
			break
		}
	}
	if !found {
		fmt.Println("Service installed but not running (plist exists, not in launchctl).")
		fmt.Println("Run 'joycode-proxy service install' to start it.")
	}

	fmt.Printf("\nLogs: %s/\n", filepath.Join(home, logDir))
	return nil
}
```

- [ ] **Step 3: 重写 service.go — 平台无关的命令注册，调用平台函数**

文件: `cmd/JoyCodeProxy/service.go`（替换整个文件）

```go
// cmd/JoyCodeProxy/service.go

package main

import (
	"github.com/spf13/cobra"
)

const (
	serviceLabel = "com.joycode.proxy"
	plistName    = serviceLabel + ".plist"
	logDir       = ".joycode-proxy/logs"
)

var serviceCmd = &cobra.Command{
	Use:     "service",
	Short:   "管理后台服务（安装/卸载/状态）",
	Long:    "将 JoyCode Proxy 安装为系统后台服务，支持开机自启和崩溃自动重启。自动适配 macOS (launchd) 和 Linux (systemd)。",
	GroupID: "service",
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "安装并启动后台服务",
	Long:  "将代理安装为系统后台服务。安装后自动启动，支持开机自启和崩溃自动重启。",
	Example: `  # 使用默认端口 34891 安装
  joycode-proxy service install

  # 指定端口
  joycode-proxy service install -p 8080`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return installService(servePort)
	},
}

var serviceUninstallCmd = &cobra.Command{
	Use:     "uninstall",
	Short:   "停止并移除后台服务",
	Example: `  joycode-proxy service uninstall`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return uninstallService()
	},
}

var serviceStatusCmd = &cobra.Command{
	Use:     "status",
	Short:   "查看服务运行状态",
	Example: `  joycode-proxy service status`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return serviceStatus()
	},
}

func init() {
	serviceCmd.AddCommand(serviceInstallCmd)
	serviceCmd.AddCommand(serviceUninstallCmd)
	serviceCmd.AddCommand(serviceStatusCmd)
	serviceCmd.PersistentFlags().IntVarP(&servePort, "port", "p", 34891, "绑定端口")
	rootCmd.AddCommand(serviceCmd)
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 验证 Task 1 — 编译 + macOS 功能不退化**

Run: `go build -o /tmp/joycode-test ./cmd/JoyCodeProxy/ && /tmp/joycode-test service --help`
Expected:
  - Exit code: 0
  - Output contains: "install" and "uninstall" and "status"
  - 当前 macOS 服务不受影响（plist 未被删除）

- [ ] **Step 5: 提交**

Run: `git add cmd/JoyCodeProxy/service.go cmd/JoyCodeProxy/service_darwin.go cmd/JoyCodeProxy/service_linux.go && git commit -m "refactor(cli): extract platform-specific service install to build-tagged files"`

---

### Task 2: 实现守护进程模式 — Supervisor 子进程管理 + 崩溃自动重启

**Depends on:** None
**Files:**
- Create: `cmd/JoyCodeProxy/daemon.go`
- Create: `cmd/JoyCodeProxy/daemon_test.go`

- [ ] **Step 1: 创建 daemon.go — Supervisor 进程管理器 + CLI 命令**

守护进程模式使用 supervisor re-exec 模式：主进程 fork 自己作为子进程运行 `serve`，主进程监控子进程，崩溃时指数退避重启。PID 文件保证唯一实例。

```go
// cmd/JoyCodeProxy/daemon.go

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

const (
	daemonEnvKey    = "_JOYCODE_DAEMON_CHILD"
	pidFileName     = ".joycode-proxy/daemon.pid"
	logFileName     = ".joycode-proxy/logs/daemon.log"
	maxRestartDelay = 30 * time.Second
	baseRestartDelay = 1 * time.Second
)

var (
	daemonLogFile string
	daemonPIDFile string
)

var daemonCmd = &cobra.Command{
	Use:     "daemon",
	Short:   "以守护进程模式运行（崩溃自动重启）",
	Long:    "以后台守护进程模式启动代理服务。自动在后台运行，崩溃后自动重启（指数退避），日志写入文件。",
	GroupID: "service",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "启动守护进程",
	Long: "启动 JoyCode Proxy 守护进程。主进程作为 supervisor 监控子进程，" +
		"子进程崩溃时自动重启（1s → 2s → 4s → ... → 30s 指数退避）。",
	Example: `  # 使用默认端口启动
  joycode-proxy daemon start

  # 指定端口
  joycode-proxy daemon start -p 8080`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return startDaemon()
	},
}

var daemonStopCmd = &cobra.Command{
	Use:     "stop",
	Short:   "停止守护进程",
	Example: `  joycode-proxy daemon stop`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return stopDaemon()
	},
}

var daemonRestartCmd = &cobra.Command{
	Use:     "restart",
	Short:   "重启守护进程",
	Example: `  joycode-proxy daemon restart`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := stopDaemon(); err != nil {
			log.Printf("stop warning: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
		return startDaemon()
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:     "status",
	Short:   "查看守护进程状态",
	Example: `  joycode-proxy daemon status`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return daemonStatus()
	},
}

var daemonLogsCmd = &cobra.Command{
	Use:     "logs",
	Short:   "查看守护进程日志（最后 N 行）",
	Example: `  joycode-proxy daemon logs
  joycode-proxy daemon logs -n 50`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return tailDaemonLogs(daemonLines)
	},
}

var daemonLines int

func init() {
	home, _ := os.UserHomeDir()
	daemonPIDFile = filepath.Join(home, pidFileName)
	daemonLogFile = filepath.Join(home, logFileName)

	daemonLogsCmd.Flags().IntVarP(&daemonLines, "lines", "n", 20, "显示最后 N 行日志")

	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonLogsCmd)
	daemonCmd.PersistentFlags().IntVarP(&servePort, "port", "p", 34891, "绑定端口")
	rootCmd.AddCommand(daemonCmd)
}

// startDaemon forks the current binary as a supervisor that monitors the child.
func startDaemon() error {
	if os.Getenv(daemonEnvKey) != "" {
		return fmt.Errorf("already running as daemon child (nested start not allowed)")
	}

	// Check for existing daemon
	if pid, running := checkRunningDaemon(); running {
		return fmt.Errorf("daemon already running (PID %d). Use 'daemon restart' or 'daemon stop' first", pid)
	}

	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine binary path: %w", err)
	}

	// Ensure log directory exists
	if err := os.MkdirAll(filepath.Dir(daemonLogFile), 0755); err != nil {
		return fmt.Errorf("cannot create log directory: %w", err)
	}

	// Start supervisor process (detached)
	args := []string{
		"serve", "--port", strconv.Itoa(servePort),
	}
	if verbose {
		args = append(args, "-v")
	}
	if skipValidation {
		args = append(args, "--skip-validation")
	}

	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(), daemonEnvKey+"=1")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Write PID file
	pidData := daemonPID{
		PID:       cmd.Process.Pid,
		Port:      servePort,
		StartedAt: time.Now().Format(time.RFC3339),
	}
	writePIDFile(pidData)

	// Detach from the child — let it run independently
	cmd.Process.Release()

	fmt.Printf("Daemon started (PID %d, port %d)\n", cmd.Process.Pid, servePort)
	fmt.Printf("  Logs: %s\n", daemonLogFile)
	fmt.Printf("  PID:  %s\n", daemonPIDFile)
	return nil
}

// runAsDaemonChild is called from serve.go when _JOYCODE_DAEMON_CHILD=1.
// It wraps the actual serve with crash logging and restart signaling.
// The parent (supervisor) is the process that called startDaemon — but since
// we detached, the child IS the actual server process. The supervisor role
// is handled by a wrapper main() check.
func runAsDaemonChild() {
	// Set up log file for crash output
	logFile, err := os.OpenFile(daemonLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("[daemon] cannot open log file: %v", err)
	}
	defer logFile.Close()
	log.SetOutput(logFile)
	log.Printf("[daemon] child process started (PID %d)", os.Getpid())
}

func stopDaemon() error {
	pidData, err := readPIDFile()
	if err != nil {
		fmt.Println("Daemon not running (PID file not found).")
		return nil
	}

	proc, err := os.FindProcess(pidData.PID)
	if err != nil {
		removePIDFile()
		fmt.Println("Daemon not running (process not found).")
		return nil
	}

	// Send SIGTERM
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		removePIDFile()
		fmt.Printf("Daemon process %d not responding: %v\n", pidData.PID, err)
		return nil
	}

	// Wait for process to exit (with timeout)
	done := make(chan error, 1)
	go func() {
		_, err := proc.Wait()
		done <- err
	}()

	select {
	case <-done:
		// Process exited
	case <-time.After(5 * time.Second):
		// Force kill
		proc.Signal(syscall.SIGKILL)
	}

	removePIDFile()
	fmt.Printf("Daemon stopped (was PID %d)\n", pidData.PID)
	return nil
}

func daemonStatus() error {
	pidData, err := readPIDFile()
	if err != nil {
		fmt.Println("Daemon not running (no PID file).")
		return nil
	}

	proc, err := os.FindProcess(pidData.PID)
	if err != nil {
		fmt.Printf("Daemon PID %d — process lookup failed\n", pidData.PID)
		return nil
	}

	// Check if process is actually running
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		fmt.Printf("Daemon PID %d — NOT running (stale PID file)\n", pidData.PID)
		removePIDFile()
		return nil
	}

	fmt.Printf("Daemon running (PID %d, port %d, started %s)\n",
		pidData.PID, pidData.Port, pidData.StartedAt)
	fmt.Printf("  Logs: %s\n", daemonLogFile)
	return nil
}

func tailDaemonLogs(n int) error {
	data, err := os.ReadFile(daemonLogFile)
	if err != nil {
		fmt.Println("No daemon log file found.")
		return nil
	}
	lines := splitLines(string(data))
	start := len(lines) - n
	if start < 0 {
		start = 0
	}
	for _, line := range lines[start:] {
		fmt.Println(line)
	}
	return nil
}

// --- PID file management ---

type daemonPID struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	StartedAt string `json:"started_at"`
}

func writePIDFile(data daemonPID) error {
	if err := os.MkdirAll(filepath.Dir(daemonPIDFile), 0755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(data, "", "  ")
	return os.WriteFile(daemonPIDFile, b, 0644)
}

func readPIDFile() (daemonPID, error) {
	var data daemonPID
	b, err := os.ReadFile(daemonPIDFile)
	if err != nil {
		return data, err
	}
	err = json.Unmarshal(b, &data)
	return data, err
}

func removePIDFile() {
	os.Remove(daemonPIDFile)
}

func checkRunningDaemon() (int, bool) {
	data, err := readPIDFile()
	if err != nil {
		return 0, false
	}
	proc, err := os.FindProcess(data.PID)
	if err != nil {
		return 0, false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		removePIDFile()
		return 0, false
	}
	return data.PID, true
}

// --- Supervisor mode ---

// RunSupervisor starts a supervisor loop that spawns and monitors the child process.
// Called when the binary is invoked with _JOYCODE_DAEMON_SUPERVISOR=1.
func RunSupervisor(port int) {
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, logFileName)
	os.MkdirAll(filepath.Dir(logPath), 0755)

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("[supervisor] cannot open log: %v", err)
	}
	defer logFile.Close()
	log.SetOutput(logFile)

	log.Printf("[supervisor] starting (PID %d, port %d)", os.Getpid(), port)

	// Write PID file
	writePIDFile(daemonPID{
		PID:       os.Getpid(),
		Port:      port,
		StartedAt: time.Now().Format(time.RFC3339),
	})

	// Handle supervisor shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var mu sync.Mutex
	delay := baseRestartDelay

	for {
		binPath, err := os.Executable()
		if err != nil {
			log.Fatalf("[supervisor] cannot find binary: %v", err)
		}

		args := []string{"serve", "--port", strconv.Itoa(port)}
		cmd := exec.Command(binPath, args...)
		cmd.Env = append(os.Environ(), daemonEnvKey+"=1")
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		log.Printf("[supervisor] spawning child process")
		if err := cmd.Start(); err != nil {
			log.Printf("[supervisor] failed to start child: %v", err)
			mu.Lock()
			time.Sleep(delay)
			delay = min(delay*2, maxRestartDelay)
			mu.Unlock()
			continue
		}

		// Wait for child to exit or signal
		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		select {
		case err := <-done:
			if err != nil {
				log.Printf("[supervisor] child exited with error: %v — restarting in %v", err, delay)
			} else {
				log.Printf("[supervisor] child exited cleanly — restarting in %v", delay)
			}
			mu.Lock()
			time.Sleep(delay)
			delay = min(delay*2, maxRestartDelay)
			mu.Unlock()

		case sig := <-sigCh:
			log.Printf("[supervisor] received %v — shutting down", sig)
			cmd.Process.Signal(syscall.SIGTERM)
			cmd.Wait()
			removePIDFile()
			log.Printf("[supervisor] stopped")
			return
		}
	}
}
```

- [ ] **Step 2: 创建 daemon_test.go — 守护进程核心逻辑单元测试**

```go
// cmd/JoyCodeProxy/daemon_test.go

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDaemonPID_WriteRead(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	original := daemonPID{
		PID:       12345,
		Port:      34891,
		StartedAt: time.Now().Format(time.RFC3339),
	}

	// Write
	b, _ := json.MarshalIndent(original, "", "  ")
	if err := os.WriteFile(pidFile, b, 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Read
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	var loaded daemonPID
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if loaded.PID != original.PID {
		t.Errorf("PID = %d, want %d", loaded.PID, original.PID)
	}
	if loaded.Port != original.Port {
		t.Errorf("Port = %d, want %d", loaded.Port, original.Port)
	}
}

func TestCheckRunningDaemon_NoPIDFile(t *testing.T) {
	// Point to temp dir with no PID file
	oldPIDFile := daemonPIDFile
	daemonPIDFile = filepath.Join(t.TempDir(), "nonexistent.pid")
	defer func() { daemonPIDFile = oldPIDFile }()

	pid, running := checkRunningDaemon()
	if running {
		t.Errorf("expected not running, got PID %d running=true", pid)
	}
}

func TestCheckRunningDaemon_StalePIDFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "stale.pid")

	// Write a PID that definitely doesn't exist
	data := daemonPID{PID: 999999998, Port: 34891, StartedAt: time.Now().Format(time.RFC3339)}
	b, _ := json.Marshal(data)
	os.WriteFile(pidFile, b, 0644)

	oldPIDFile := daemonPIDFile
	daemonPIDFile = pidFile
	defer func() { daemonPIDFile = oldPIDFile }()

	pid, running := checkRunningDaemon()
	if running {
		t.Errorf("expected stale PID to be detected, got PID %d running=true", pid)
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input  string
		expect int
	}{
		{"line1\nline2\nline3", 3},
		{"", 0},
		{"single", 1},
		{"a\nb\n", 2},
	}
	for _, tt := range tests {
		got := splitLines(tt.input)
		if len(got) != tt.expect {
			t.Errorf("splitLines(%q) = %d lines, want %d", tt.input, len(got), tt.expect)
		}
	}
}

func TestContainsStr(t *testing.T) {
	if !containsStr("hello world", "world") {
		t.Error("expected true for 'world' in 'hello world'")
	}
	if containsStr("hello", "world") {
		t.Error("expected false for 'world' in 'hello'")
	}
}
```

- [ ] **Step 3: 修改 main.go — 注册 daemon 命令组**

文件: `cmd/JoyCodeProxy/main.go`（替换整个文件）

```go
// cmd/JoyCodeProxy/main.go

package main

import (
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

func main() {
	// Check if we're being invoked as daemon supervisor
	if os.Getenv("_JOYCODE_DAEMON_SUPERVISOR") == "1" {
		port, _ := strconv.Atoi(os.Getenv("_JOYCODE_DAEMON_PORT"))
		if port == 0 {
			port = 34891
		}
		RunSupervisor(port)
		return
	}

	rootCmd.AddGroup(
		&cobra.Group{ID: "core", Title: "Core Commands:"},
		&cobra.Group{ID: "service", Title: "Service Management:"},
		&cobra.Group{ID: "query", Title: "Query & Info:"},
	)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 4: 修改 serve.go — 检测 daemon child 模式并配置日志**

文件: `cmd/JoyCodeProxy/serve.go:39-43`（在 `RunE` 函数体开头插入 daemon 检测）

在 `resolveClient()` 调用之前添加：

```go
// cmd/JoyCodeProxy/serve.go — RunE 函数体开头（第 40 行之后）

// If running as daemon child, redirect logs to daemon log file
if os.Getenv("_JOYCODE_DAEMON_CHILD") == "1" {
    runAsDaemonChild()
}
```

- [ ] **Step 5: 修改 daemon.go startDaemon — 使用 supervisor 模式**

替换 `startDaemon` 函数中直接 fork 子进程的方式，改为 fork 一个 supervisor 进程：

文件: `cmd/JoyCodeProxy/daemon.go` — `startDaemon` 函数（替换整个函数）

```go
// startDaemon forks a supervisor process that monitors the server child.
func startDaemon() error {
	if os.Getenv(daemonEnvKey) != "" || os.Getenv("_JOYCODE_DAEMON_SUPERVISOR") == "1" {
		return fmt.Errorf("already running as daemon (nested start not allowed)")
	}

	if pid, running := checkRunningDaemon(); running {
		return fmt.Errorf("daemon already running (PID %d). Use 'daemon restart' or 'daemon stop' first", pid)
	}

	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine binary path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(daemonLogFile), 0755); err != nil {
		return fmt.Errorf("cannot create log directory: %w", err)
	}

	// Start supervisor process
	cmd := exec.Command(binPath)
	cmd.Env = append(os.Environ(),
		"_JOYCODE_DAEMON_SUPERVISOR=1",
		"_JOYCODE_DAEMON_PORT="+strconv.Itoa(servePort),
	)
	if verbose {
		cmd.Env = append(cmd.Env, "_JOYCODE_DAEMON_VERBOSE=1")
	}
	if skipValidation {
		cmd.Env = append(cmd.Env, "_JOYCODE_DAEMON_SKIP_VALIDATION=1")
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon supervisor: %w", err)
	}

	cmd.Process.Release()

	// Wait briefly for PID file to be written
	time.Sleep(200 * time.Millisecond)
	pidData, err := readPIDFile()
	if err != nil {
		fmt.Printf("Daemon supervisor started (PID %d, port %d)\n", cmd.Process.Pid, servePort)
	} else {
		fmt.Printf("Daemon started (PID %d, port %d)\n", pidData.PID, pidData.Port)
	}
	fmt.Printf("  Logs: %s\n", daemonLogFile)
	fmt.Printf("  PID:  %s\n", daemonPIDFile)
	return nil
}
```

同步修改 `RunSupervisor` 中的 args 构造，读取环境变量传递 verbose 和 skip-validation：

文件: `cmd/JoyCodeProxy/daemon.go` — `RunSupervisor` 函数中的 args 构造部分

```go
// 在 RunSupervisor 函数中，构建 args 的部分替换为：

args := []string{"serve", "--port", strconv.Itoa(port)}
if os.Getenv("_JOYCODE_DAEMON_VERBOSE") == "1" {
    args = append(args, "-v")
}
if os.Getenv("_JOYCODE_DAEMON_SKIP_VALIDATION") == "1" {
    args = append(args, "--skip-validation")
}
```

- [ ] **Step 6: 验证 Task 2 — 编译 + 单元测试**

Run: `go build -o /tmp/joycode-test ./cmd/JoyCodeProxy/ && /tmp/joycode-test daemon --help`
Expected:
  - Exit code: 0
  - Output contains: "start" and "stop" and "restart" and "status" and "logs"

Run: `go test ./cmd/JoyCodeProxy/ -v -count=1`
Expected:
  - Exit code: 0
  - Output contains: "PASS" for all tests

- [ ] **Step 7: 提交**

Run: `git add cmd/JoyCodeProxy/daemon.go cmd/JoyCodeProxy/daemon_test.go cmd/JoyCodeProxy/main.go cmd/JoyCodeProxy/serve.go && git commit -m "feat(cli): add daemon mode with supervisor, crash recovery and PID management"`

---

### Task 3: 集成测试 — 端到端验证 Daemon 和 Service 命令

**Depends on:** Task 1, Task 2
**Files:**
- Create: `cmd/JoyCodeProxy/integration_test.go`

- [ ] **Step 1: 创建集成测试 — 验证 daemon start/stop/status 完整流程**

```go
// cmd/JoyCodeProxy/integration_test.go

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func buildTestBinary(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "joycode-proxy-test")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/JoyCodeProxy/")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s: %v", string(output), err)
	}
	return binPath
}

func TestDaemonStartStop(t *testing.T) {
	bin := buildTestBinary(t)
	tmpDir := t.TempDir()

	// Override PID and log paths via home dir
	origPID := daemonPIDFile
	origLog := daemonLogFile
	daemonPIDFile = filepath.Join(tmpDir, "daemon.pid")
	daemonLogFile = filepath.Join(tmpDir, "daemon.log")
	defer func() {
		daemonPIDFile = origPID
		daemonLogFile = origLog
	}()

	// Start daemon
	cmd := exec.Command(bin, "daemon", "start", "--port", "34892")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("daemon start failed: %s: %v", string(output), err)
	}

	// Wait for daemon to start
	time.Sleep(1 * time.Second)

	// Check status
	cmd = exec.Command(bin, "daemon", "status")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("daemon status failed: %s: %v", string(output), err)
	}
	statusStr := string(output)
	if !containsStr(statusStr, "running") {
		t.Errorf("expected 'running' in status, got: %s", statusStr)
	}

	// Stop daemon
	cmd = exec.Command(bin, "daemon", "stop")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Logf("daemon stop output: %s", string(output))
	}

	// Wait for cleanup
	time.Sleep(500 * time.Millisecond)
}

func TestServiceHelpOutput(t *testing.T) {
	bin := buildTestBinary(t)

	tests := []struct {
		cmd    string
		expect string
	}{
		{"service", "install"},
		{"service", "uninstall"},
		{"service", "status"},
		{"daemon", "start"},
		{"daemon", "stop"},
		{"daemon", "restart"},
		{"daemon", "status"},
		{"daemon", "logs"},
	}

	for _, tt := range tests {
		t.Run(tt.cmd+"_"+tt.expect, func(t *testing.T) {
			cmd := exec.Command(bin, tt.cmd, "--help")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("--help failed: %v", err)
			}
			if !containsStr(string(output), tt.expect) {
				t.Errorf("expected '%s' in help output, got: %s", tt.expect, string(output))
			}
		})
	}
}

func TestDaemonDoubleStart(t *testing.T) {
	bin := buildTestBinary(t)
	tmpDir := t.TempDir()

	origPID := daemonPIDFile
	origLog := daemonLogFile
	daemonPIDFile = filepath.Join(tmpDir, "daemon.pid")
	daemonLogFile = filepath.Join(tmpDir, "daemon.log")
	defer func() {
		daemonPIDFile = origPID
		daemonLogFile = origLog
	}()

	// Start daemon first time
	cmd := exec.Command(bin, "daemon", "start", "--port", "34893")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("first start failed: %s: %v", string(output), err)
	}
	time.Sleep(1 * time.Second)

	// Try starting again — should fail
	cmd = exec.Command(bin, "daemon", "start", "--port", "34893")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	output, _ := cmd.CombinedOutput()
	if !containsStr(string(output), "already running") {
		t.Errorf("expected 'already running' error, got: %s", string(output))
	}

	// Cleanup
	cmd = exec.Command(bin, "daemon", "stop")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	cmd.Run()
	time.Sleep(500 * time.Millisecond)
}
```

- [ ] **Step 2: 验证 Task 3 — 运行集成测试**

Run: `go test ./cmd/JoyCodeProxy/ -v -run TestServiceHelpOutput -count=1`
Expected:
  - Exit code: 0
  - Output contains: "PASS" for all help tests

- [ ] **Step 3: 提交**

Run: `git add cmd/JoyCodeProxy/integration_test.go && git commit -m "test(cli): add integration tests for daemon start/stop and service help"`

---

### Task 4: 全量验证 — 编译 + 测试 + 服务部署

**Depends on:** Task 3
**Files:** None (verification only)

- [ ] **Step 1: 全量编译和测试**

Run: `go build ./... && go test ./... -count=1`
Expected:
  - Exit code: 0
  - All packages PASS
  - No compilation errors

- [ ] **Step 2: 构建正式二进制并验证 daemon 命令**

Run: `go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/ && ./joycode_proxy_bin daemon --help`
Expected:
  - Exit code: 0
  - Output contains: "start", "stop", "restart", "status", "logs"
  - Binary size < 30MB

- [ ] **Step 3: 验证 service 命令不退化**

Run: `./joycode_proxy_bin service --help`
Expected:
  - Exit code: 0
  - Output contains: "install", "uninstall", "status"

- [ ] **Step 4: 验证 macOS 当前服务不受影响**

Run: `./joycode_proxy_bin service status`
Expected:
  - Exit code: 0
  - Shows current service status (running or not)

Run: `curl -s http://localhost:34891/health`
Expected:
  - HTTP 200
  - JSON with "status": "ok"

- [ ] **Step 5: 提交最终状态**

Run: `git add -A && git status`
Expected:
  - No uncommitted changes (all committed in Tasks 1-3)
