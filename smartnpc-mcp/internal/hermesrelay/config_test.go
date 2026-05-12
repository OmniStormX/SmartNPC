package hermesrelay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigFile_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.yaml")
	yaml := `profiles:
  - name: xiami
    npc_filter: XiaMi
    gateway_url: http://127.0.0.1:8642
    conversation: xiami
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY
  - name: abigail
    npc_filter: Abigail
    gateway_url: http://127.0.0.1:8643
    conversation: abigail
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Setenv("SMARTNPC_HERMES_KEY", "test-bearer")

	cfgs, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if len(cfgs) != 2 {
		t.Fatalf("want 2 cfgs, got %d", len(cfgs))
	}
	if cfgs[0].URL != "http://127.0.0.1:8642" {
		t.Errorf("xiami URL = %q", cfgs[0].URL)
	}
	if cfgs[0].NPCName != "XiaMi" {
		t.Errorf("xiami NPCName = %q", cfgs[0].NPCName)
	}
	if cfgs[0].APIKey != "test-bearer" {
		t.Errorf("xiami APIKey not resolved from env: %q", cfgs[0].APIKey)
	}
	if cfgs[1].Conversation != "abigail" {
		t.Errorf("abigail Conversation = %q", cfgs[1].Conversation)
	}
}

func TestLoadConfigFile_MissingEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.yaml")
	yaml := `profiles:
  - name: xiami
    npc_filter: XiaMi
    gateway_url: http://127.0.0.1:8642
    conversation: xiami
    model: hermes-agent
    api_key_env: MISSING_VAR_XYZ
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	os.Unsetenv("MISSING_VAR_XYZ")

	cfgs, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if cfgs[0].APIKey != "" {
		t.Errorf("missing env should leave APIKey empty, got %q", cfgs[0].APIKey)
	}
}

func TestLoadConfigFile_NoProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.yaml")
	if err := os.WriteFile(path, []byte("profiles: []\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := LoadConfigFile(path)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("want empty-profile error, got %v", err)
	}
}

func TestLoadConfigFile_BadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.yaml")
	if err := os.WriteFile(path, []byte("not: [valid yaml"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := LoadConfigFile(path)
	if err == nil {
		t.Errorf("want parse error, got nil")
	}
}
