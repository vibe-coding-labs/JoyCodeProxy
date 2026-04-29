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
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s: %v", string(output), err)
	}
	return binPath
}

func TestDaemonStartStop(t *testing.T) {
	bin := buildTestBinary(t)
	tmpDir := t.TempDir()

	// Start daemon
	cmd := exec.Command(bin, "daemon", "start", "--port", "34892")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("daemon start failed: %s: %v", string(output), err)
	}
	if !containsStr(string(output), "Daemon") {
		t.Errorf("expected 'Daemon' in start output, got: %s", string(output))
	}

	// Wait for daemon to start
	time.Sleep(2 * time.Second)

	// Check status
	cmd = exec.Command(bin, "daemon", "status")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("daemon status failed: %s: %v", string(output), err)
	}
	if !containsStr(string(output), "running") {
		t.Errorf("expected 'running' in status, got: %s", string(output))
	}

	// Stop daemon
	cmd = exec.Command(bin, "daemon", "stop")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	output, _ = cmd.CombinedOutput()
	t.Logf("stop output: %s", string(output))
	time.Sleep(500 * time.Millisecond)
}

func TestServiceHelpOutput(t *testing.T) {
	bin := buildTestBinary(t)

	tests := []struct {
		args   []string
		expect string
	}{
		{[]string{"service", "--help"}, "install"},
		{[]string{"service", "--help"}, "uninstall"},
		{[]string{"service", "--help"}, "status"},
		{[]string{"daemon", "--help"}, "start"},
		{[]string{"daemon", "--help"}, "stop"},
		{[]string{"daemon", "--help"}, "restart"},
		{[]string{"daemon", "--help"}, "status"},
		{[]string{"daemon", "--help"}, "logs"},
	}

	for _, tt := range tests {
		t.Run(tt.args[0]+"_"+tt.expect, func(t *testing.T) {
			cmd := exec.Command(bin, tt.args...)
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

	// Start daemon first time
	cmd := exec.Command(bin, "daemon", "start", "--port", "34893")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("first start failed: %s: %v", string(output), err)
	}
	time.Sleep(2 * time.Second)

	// Try starting again — should fail (SilenceErrors suppresses output)
	cmd = exec.Command(bin, "daemon", "start", "--port", "34893")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Errorf("expected second start to fail, but succeeded. output: %s", string(output))
	}

	// Cleanup
	cmd = exec.Command(bin, "daemon", "stop")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	cmd.Run()
	time.Sleep(500 * time.Millisecond)
}
