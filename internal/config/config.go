package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	DefaultSessionPath         = ".sessions/user.json"
	DefaultTranscriptOutputDir = "artifacts/transcripts"
	// Auto-selected visible history window.
	// It is large enough to preserve the current scenario context, transient bot
	// messages, and pin/service events, while keeping getHistory payloads modest.
	DefaultHistoryWindow    = 64
	DefaultSyncIntervalMS   = 1600
	DefaultActionSpacingMS  = 3000
	DefaultRPCSpacingMS     = 700
	DefaultPinnedCacheTTLMS = 45000
)

type Config struct {
	AppID               int
	AppHash             string
	Phone               string
	Password            string
	SessionPath         string
	TranscriptOutputDir string
	HistoryWindow       int
	SyncInterval        time.Duration
	ActionSpacing       time.Duration
	RPCSpacing          time.Duration
	PinnedCacheTTL      time.Duration
}

func Load() (Config, error) {
	appID, err := intFromEnv("TG_E2E_APP_ID", 0)
	if err != nil {
		return Config{}, err
	}
	return Config{
		AppID:               appID,
		AppHash:             stringsOrEmpty(os.Getenv("TG_E2E_APP_HASH")),
		Phone:               stringsOrEmpty(os.Getenv("TG_E2E_PHONE")),
		Password:            stringsOrEmpty(os.Getenv("TG_E2E_PASSWORD")),
		SessionPath:         defaultString(os.Getenv("TG_E2E_SESSION_PATH"), DefaultSessionPath),
		TranscriptOutputDir: defaultString(os.Getenv("TG_E2E_TRANSCRIPT_DIR"), DefaultTranscriptOutputDir),
		HistoryWindow:       DefaultHistoryWindow,
		SyncInterval:        time.Duration(DefaultSyncIntervalMS) * time.Millisecond,
		ActionSpacing:       time.Duration(DefaultActionSpacingMS) * time.Millisecond,
		RPCSpacing:          time.Duration(DefaultRPCSpacingMS) * time.Millisecond,
		PinnedCacheTTL:      time.Duration(DefaultPinnedCacheTTLMS) * time.Millisecond,
	}, nil
}

func (c Config) ValidateRuntime() error {
	if c.AppID == 0 {
		return fmt.Errorf("TG_E2E_APP_ID is required")
	}
	if c.AppHash == "" {
		return fmt.Errorf("TG_E2E_APP_HASH is required")
	}
	if c.SessionPath == "" {
		return fmt.Errorf("TG_E2E_SESSION_PATH is required")
	}
	if c.HistoryWindow <= 0 {
		return fmt.Errorf("history window must be > 0")
	}
	if c.SyncInterval <= 0 {
		return fmt.Errorf("sync interval must be > 0")
	}
	if c.ActionSpacing <= 0 {
		return fmt.Errorf("action spacing must be > 0")
	}
	if c.RPCSpacing <= 0 {
		return fmt.Errorf("rpc spacing must be > 0")
	}
	if c.PinnedCacheTTL <= 0 {
		return fmt.Errorf("pinned cache ttl must be > 0")
	}
	return nil
}

func (c Config) ValidateLogin() error {
	if err := c.ValidateRuntime(); err != nil {
		return err
	}
	if c.Phone == "" {
		return fmt.Errorf("TG_E2E_PHONE is required for login")
	}
	return nil
}

func (c Config) RuntimeLockPath() string {
	sessionPath := c.SessionPath
	if sessionPath == "" {
		sessionPath = DefaultSessionPath
	}
	dir := filepath.Dir(sessionPath)
	if dir == "." || dir == "" {
		return "runtime.lock"
	}
	return filepath.Join(dir, "runtime.lock")
}

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func intFromEnv(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return n, nil
}

func stringsOrEmpty(v string) string {
	return v
}
