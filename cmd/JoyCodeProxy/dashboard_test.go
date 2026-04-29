package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDashboardEndpoints(t *testing.T) {
	bin := buildTestBinary(t)
	port := 34896

	// Start server with real HOME (for JoyCode credentials) + skip validation
	cmd := exec.Command(bin, "serve", "--port", fmt.Sprintf("%d", port), "--skip-validation", "--ptkey", "test", "--userid", "test")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	// Wait for server to be ready
	time.Sleep(3 * time.Second)

	base := fmt.Sprintf("http://localhost:%d", port)

	// Test health endpoint
	t.Run("health", func(t *testing.T) {
		resp, err := http.Get(base + "/api/health")
		if err != nil {
			t.Fatalf("health request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		var m map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&m)
		if m["status"] != "ok" {
			t.Errorf("status = %v, want ok", m["status"])
		}
	})

	// Test add account
	t.Run("add_account", func(t *testing.T) {
		body := `{"api_key":"test-integration","pt_key":"test-pt","user_id":"test-user","is_default":true}`
		resp, err := http.Post(base+"/api/accounts", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("add account: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	// Test list accounts
	t.Run("list_accounts", func(t *testing.T) {
		resp, err := http.Get(base + "/api/accounts")
		if err != nil {
			t.Fatalf("list accounts: %v", err)
		}
		defer resp.Body.Close()
		var m map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&m)
		accounts := m["accounts"].([]interface{})
		if len(accounts) < 1 {
			t.Errorf("accounts len = %d, want >= 1", len(accounts))
		}
	})

	// Test models
	t.Run("models", func(t *testing.T) {
		resp, err := http.Get(base + "/api/models")
		if err != nil {
			t.Fatalf("models: %v", err)
		}
		defer resp.Body.Close()
		var m map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&m)
		models := m["models"].([]interface{})
		if len(models) == 0 {
			t.Error("expected at least 1 model")
		}
	})

	// Test stats
	t.Run("stats", func(t *testing.T) {
		resp, err := http.Get(base + "/api/stats")
		if err != nil {
			t.Fatalf("stats: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	// Test settings
	t.Run("settings", func(t *testing.T) {
		resp, err := http.Get(base + "/api/settings")
		if err != nil {
			t.Fatalf("settings: %v", err)
		}
		defer resp.Body.Close()
		var m map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&m)
		if _, ok := m["settings"]; !ok {
			t.Error("missing settings field")
		}
	})

	// Test delete account
	t.Run("delete_account", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", base+"/api/accounts/test-integration", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})
}

func TestStaticFileServing(t *testing.T) {
	bin := buildTestBinary(t)
	port := 34897

	cmd := exec.Command(bin, "serve", "--port", fmt.Sprintf("%d", port), "--skip-validation", "--ptkey", "test", "--userid", "test")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Start()
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()
	time.Sleep(3 * time.Second)

	base := fmt.Sprintf("http://localhost:%d", port)

	// Test index.html
	t.Run("index_html", func(t *testing.T) {
		resp, err := http.Get(base + "/")
		if err != nil {
			t.Fatalf("get index: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	// Test SPA fallback
	t.Run("spa_fallback", func(t *testing.T) {
		resp, err := http.Get(base + "/accounts")
		if err != nil {
			t.Fatalf("get accounts page: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	// Test favicon
	t.Run("favicon", func(t *testing.T) {
		resp, err := http.Get(base + "/favicon.svg")
		if err != nil {
			t.Fatalf("get favicon: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	// Test /health (OpenAI handler)
	t.Run("health_endpoint", func(t *testing.T) {
		resp, err := http.Get(base + "/health")
		if err != nil {
			t.Fatalf("get health: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})
}

func TestOpenAPIEndpoints(t *testing.T) {
	bin := buildTestBinary(t)
	port := 34898

	cmd := exec.Command(bin, "serve", "--port", fmt.Sprintf("%d", port), "--skip-validation", "--ptkey", "test", "--userid", "test")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Start()
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()
	time.Sleep(3 * time.Second)

	base := fmt.Sprintf("http://localhost:%d", port)

	// Test /v1/models (OpenAI endpoint)
	t.Run("v1_models", func(t *testing.T) {
		resp, err := http.Get(base + "/v1/models")
		if err != nil {
			t.Fatalf("v1 models: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	// Test /health (OpenAI handler)
	t.Run("health", func(t *testing.T) {
		resp, err := http.Get(base + "/health")
		if err != nil {
			t.Fatalf("health: %v", err)
		}
		defer resp.Body.Close()
		var m map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&m)
		if m["status"] != "ok" {
			t.Errorf("status = %v, want ok", m["status"])
		}
	})
}
