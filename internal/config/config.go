package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	DefaultSessionPath         = ".sessions/user.json"
	DefaultTranscriptOutputDir = "artifacts/transcripts"
	DefaultHistoryLimit        = 50
	DefaultSyncIntervalMS      = 1200
)

type Config struct {
	AppID               int
	AppHash             string
	Phone               string
	Password            string
	SessionPath         string
	TranscriptOutputDir string
	DefaultChat         string
	HistoryLimit        int
	SyncInterval        time.Duration
}

func Load() (Config, error) {
	appID, err := intFromEnv("TG_E2E_APP_ID", 0)
	if err != nil {
		return Config{}, err
	}
	historyLimit, err := intFromEnv("TG_E2E_HISTORY_LIMIT", DefaultHistoryLimit)
	if err != nil {
		return Config{}, err
	}
	syncIntervalMS, err := intFromEnv("TG_E2E_SYNC_INTERVAL_MS", DefaultSyncIntervalMS)
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
		DefaultChat:         stringsOrEmpty(os.Getenv("TG_E2E_DEFAULT_CHAT")),
		HistoryLimit:        historyLimit,
		SyncInterval:        time.Duration(syncIntervalMS) * time.Millisecond,
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
	if c.HistoryLimit <= 0 {
		return fmt.Errorf("TG_E2E_HISTORY_LIMIT must be > 0")
	}
	if c.SyncInterval <= 0 {
		return fmt.Errorf("TG_E2E_SYNC_INTERVAL_MS must be > 0")
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
