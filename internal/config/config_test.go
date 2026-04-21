package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadUsesBuiltInHistoryWindow(t *testing.T) {
	t.Setenv("TG_E2E_APP_ID", "123")
	t.Setenv("TG_E2E_APP_HASH", "0123456789abcdef0123456789abcdef")
	t.Setenv("TG_E2E_PHONE", "+79990000000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HistoryWindow != DefaultHistoryWindow {
		t.Fatalf("HistoryWindow = %d, want %d", cfg.HistoryWindow, DefaultHistoryWindow)
	}
}

func TestLoadUsesBuiltInPacingDefaults(t *testing.T) {
	t.Setenv("TG_E2E_APP_ID", "123")
	t.Setenv("TG_E2E_APP_HASH", "0123456789abcdef0123456789abcdef")
	t.Setenv("TG_E2E_PHONE", "+79990000000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got, want := cfg.SyncInterval.Milliseconds(), int64(DefaultSyncIntervalMS); got != want {
		t.Fatalf("SyncInterval = %dms, want %dms", got, want)
	}
	if got, want := cfg.ActionSpacing.Milliseconds(), int64(DefaultActionSpacingMS); got != want {
		t.Fatalf("ActionSpacing = %dms, want %dms", got, want)
	}
	if got, want := cfg.RPCSpacing.Milliseconds(), int64(DefaultRPCSpacingMS); got != want {
		t.Fatalf("RPCSpacing = %dms, want %dms", got, want)
	}
	if got, want := cfg.PinnedCacheTTL.Milliseconds(), int64(DefaultPinnedCacheTTLMS); got != want {
		t.Fatalf("PinnedCacheTTL = %dms, want %dms", got, want)
	}
}

func TestLoadDotEnvLoadsValuesWithoutOverridingExistingEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := []byte(strings.Join([]string{
		"# comment",
		"TG_E2E_APP_ID=123",
		"TG_E2E_APP_HASH=\"0123456789abcdef0123456789abcdef\"",
		"TG_E2E_PHONE='+79990000000'",
		"HTTP_PROXY=http://127.0.0.1:8888 # inline comment",
		"export TG_E2E_PASSWORD=secret",
		"",
	}, "\n"))
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Setenv("TG_E2E_PASSWORD", "already-set")
	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv() error = %v", err)
	}

	for key, want := range map[string]string{
		"TG_E2E_APP_ID":   "123",
		"TG_E2E_APP_HASH": "0123456789abcdef0123456789abcdef",
		"TG_E2E_PHONE":    "+79990000000",
		"HTTP_PROXY":      "http://127.0.0.1:8888",
		"TG_E2E_PASSWORD": "already-set",
	} {
		if got := os.Getenv(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestLoadDotEnvRejectsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("BROKEN_LINE\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	if err := LoadDotEnv(path); err == nil {
		t.Fatal("expected malformed .env error")
	}
}
