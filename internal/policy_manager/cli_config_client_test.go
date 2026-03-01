package policy_manager_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
)

// configPath returns a writable config.yaml path inside a temp directory.
func configPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "config.yaml")
}

func TestCLIConfigClient_SetAndGet(t *testing.T) {
	path := configPath(t)
	c := policy_manager.NewCLIConfigClientFromPath(path)

	if err := c.Set("auth.token", "my-token"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, ok := c.Get("auth.token")
	if !ok {
		t.Fatal("Get returned ok=false after Set")
	}
	if val != "my-token" {
		t.Errorf("Get = %v, want my-token", val)
	}
}

func TestCLIConfigClient_GetMissingKey(t *testing.T) {
	c := policy_manager.NewCLIConfigClientFromPath(configPath(t))
	_, ok := c.Get("does.not.exist")
	if ok {
		t.Error("Get returned ok=true for missing key")
	}
}

func TestCLIConfigClient_OverwriteValue(t *testing.T) {
	c := policy_manager.NewCLIConfigClientFromPath(configPath(t))

	c.Set("api.base_url", "http://localhost:8080")
	c.Set("api.base_url", "http://prod.example.com")

	val, ok := c.Get("api.base_url")
	if !ok {
		t.Fatal("Get returned ok=false")
	}
	if val != "http://prod.example.com" {
		t.Errorf("Get = %v, want http://prod.example.com", val)
	}
}

func TestCLIConfigClient_Delete(t *testing.T) {
	c := policy_manager.NewCLIConfigClientFromPath(configPath(t))

	c.Set("auth.token", "to-delete")
	if err := c.Delete("auth"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, ok := c.Get("auth.token")
	if ok {
		t.Error("key still present after Delete")
	}
}

func TestCLIConfigClient_PersistsAcrossInstances(t *testing.T) {
	path := configPath(t)
	c1 := policy_manager.NewCLIConfigClientFromPath(path)

	if err := c1.Set("auth.token", "persistent-tok"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Open a new client backed by the same file.
	c2 := policy_manager.NewCLIConfigClientFromPath(path)
	val, ok := c2.Get("auth.token")
	if !ok {
		t.Fatal("second client Get returned ok=false")
	}
	if val != "persistent-tok" {
		t.Errorf("second client Get = %v, want persistent-tok", val)
	}
}

func TestCLIConfigClient_Config_ContainsSetKeys(t *testing.T) {
	c := policy_manager.NewCLIConfigClientFromPath(configPath(t))

	c.Set("api.base_url", "http://localhost:8080")
	c.Set("auth.token", "tok")

	cfg := c.Config()
	if cfg.Payload == nil {
		t.Fatal("Config().Payload is nil")
	}
	// viper flattens nested keys; check top-level section exists
	if _, ok := cfg.Payload["api"]; !ok {
		t.Error("Config().Payload missing 'api' section")
	}
}

func TestCLIConfigClient_ConfigClientInfo_ContainsType(t *testing.T) {
	c := policy_manager.NewCLIConfigClientFromPath(configPath(t))
	info := c.ConfigClientInfo()
	if info == nil {
		t.Fatal("ConfigClientInfo returned nil")
	}
	if info["type"] != "CLIConfigManager" {
		t.Errorf("type = %v, want CLIConfigManager", info["type"])
	}
}

func TestCLIConfigClient_EmptyConfigFile_GivesEmptyGet(t *testing.T) {
	path := configPath(t)
	// Write an empty YAML file.
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	c := policy_manager.NewCLIConfigClientFromPath(path)
	_, ok := c.Get("auth.token")
	if ok {
		t.Error("expected ok=false for key in empty config file")
	}
}

func TestCLIConfigClient_ImplementsConfigClientInterface(t *testing.T) {
	// Compile-time check: *CLIConfigClient satisfies ConfigClient.
	var _ policy_manager.ConfigClient = policy_manager.NewCLIConfigClientFromPath(configPath(t))
}
