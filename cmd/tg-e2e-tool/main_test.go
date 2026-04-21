package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/config"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/mtproto"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/ratesweep"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/transcript"
)

func TestPathModeTreatsDefaultValueAsDefault(t *testing.T) {
	if got := pathMode(".sessions/user.json", ".sessions/user.json"); got != "default(.sessions/user.json)" {
		t.Fatalf("pathMode() = %q", got)
	}
	if got := pathMode("", ".sessions/user.json"); got != "default(.sessions/user.json)" {
		t.Fatalf("pathMode() with empty value = %q", got)
	}
	if got := pathMode("/tmp/custom.json", ".sessions/user.json"); got != "override" {
		t.Fatalf("pathMode() with custom value = %q", got)
	}
}

func TestSaveTranscriptAndArtifactMap(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{TranscriptOutputDir: dir}
	tr := transcript.New()

	artifact, err := saveScenarioArtifacts(cfg, tr, "/tmp/scenario-a.jsonl", "scenario-a", os.Stderr)
	if err != nil {
		t.Fatalf("saveScenarioArtifacts() error = %v", err)
	}
	if filepath.Base(artifact.Artifact.TranscriptJSON) != "scenario-a.json" {
		t.Fatalf("unexpected transcript json path: %s", artifact.Artifact.TranscriptJSON)
	}
	if filepath.Base(artifact.Artifact.TranscriptText) != "scenario-a.txt" {
		t.Fatalf("unexpected transcript text path: %s", artifact.Artifact.TranscriptText)
	}
	if filepath.Base(artifact.Artifact.TranscriptCompactJSON) != "scenario-a.compact.json" {
		t.Fatalf("unexpected compact transcript json path: %s", artifact.Artifact.TranscriptCompactJSON)
	}
	if filepath.Base(artifact.Artifact.TranscriptCompactText) != "scenario-a.compact.txt" {
		t.Fatalf("unexpected compact transcript text path: %s", artifact.Artifact.TranscriptCompactText)
	}

	saveLastRunArtifacts(cfg, []scenarioArtifacts{artifact}, os.Stderr)

	body, err := os.ReadFile(filepath.Join(dir, lastRunArtifactsFile))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var decoded runScenarioArtifactMap
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(decoded.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(decoded.Entries))
	}
	if decoded.Entries[0].ScenarioPath != "/tmp/scenario-a.jsonl" {
		t.Fatalf("unexpected scenario path: %s", decoded.Entries[0].ScenarioPath)
	}
	if filepath.Base(decoded.Entries[0].TranscriptCompactJSON) != "scenario-a.compact.json" {
		t.Fatalf("unexpected compact json pointer: %s", decoded.Entries[0].TranscriptCompactJSON)
	}
	if filepath.Base(decoded.LastRunSummaryJSON) != lastRunSummaryJSONFile {
		t.Fatalf("unexpected last run summary json pointer: %s", decoded.LastRunSummaryJSON)
	}
	if filepath.Base(decoded.LastRunSummaryText) != lastRunSummaryTextFile {
		t.Fatalf("unexpected last run summary text pointer: %s", decoded.LastRunSummaryText)
	}
	if decoded.LastFailureJSON != "" || decoded.LastFailureText != "" {
		t.Fatalf("expected no failure pointers on green run, got %+v", decoded)
	}
}

func TestSaveLastRunArtifactsRemovesStaleFailureAfterGreenRun(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{TranscriptOutputDir: dir}

	failed := transcript.New()
	failed.AddCommand(protocol.Command{ID: "wait", Action: "wait"})
	failed.AddEvent(protocol.Event{Type: "timeout", CommandID: "wait", Action: "wait", Error: "wait timeout after 8s"})
	failedArtifacts, err := saveScenarioArtifacts(cfg, failed, "/tmp/fail.jsonl", "fail", os.Stderr)
	if err != nil {
		t.Fatalf("saveScenarioArtifacts(failed) error = %v", err)
	}
	saveLastRunArtifacts(cfg, []scenarioArtifacts{failedArtifacts}, os.Stderr)
	if _, err := os.Stat(filepath.Join(dir, lastFailureJSONFile)); err != nil {
		t.Fatalf("expected last failure json to exist, got %v", err)
	}

	passed := transcript.New()
	passedArtifacts, err := saveScenarioArtifacts(cfg, passed, "/tmp/pass.jsonl", "pass", os.Stderr)
	if err != nil {
		t.Fatalf("saveScenarioArtifacts(passed) error = %v", err)
	}
	saveLastRunArtifacts(cfg, []scenarioArtifacts{passedArtifacts}, os.Stderr)
	if _, err := os.Stat(filepath.Join(dir, lastFailureJSONFile)); !os.IsNotExist(err) {
		t.Fatalf("expected stale last failure json to be removed, got %v", err)
	}
}

func TestParseRateSweepArgsPrepareScenario(t *testing.T) {
	opts, scenarioPaths, err := parseRateSweepArgs([]string{
		"--chat", "@examplebot",
		"--prepare-scenario", "examples/bench/00-shelfy-home-warmup.jsonl",
		"--prepare-scenario", "custom-reset.jsonl",
		"--runs", "2",
		"bench-a.jsonl",
	})
	if err != nil {
		t.Fatalf("parseRateSweepArgs() error = %v", err)
	}
	if opts.TargetChat != "@examplebot" {
		t.Fatalf("TargetChat = %q", opts.TargetChat)
	}
	if opts.Runs != 2 {
		t.Fatalf("Runs = %d", opts.Runs)
	}
	wantPrepare := []string{"examples/bench/00-shelfy-home-warmup.jsonl", "custom-reset.jsonl"}
	if !reflect.DeepEqual(opts.PreparePaths, wantPrepare) {
		t.Fatalf("PreparePaths = %#v, want %#v", opts.PreparePaths, wantPrepare)
	}
	if !reflect.DeepEqual(scenarioPaths, []string{"bench-a.jsonl"}) {
		t.Fatalf("scenarioPaths = %#v", scenarioPaths)
	}
	if opts.MinActionSpacing != time.Duration(ratesweep.DefaultMinActionSpacingMS)*time.Millisecond {
		t.Fatalf("MinActionSpacing = %s", opts.MinActionSpacing)
	}
}

func TestPrintDoctorIncludesAuthorizedLiveStatus(t *testing.T) {
	cfg := config.Config{
		AppID:               123,
		AppHash:             "0123456789abcdef0123456789abcdef",
		SessionPath:         filepath.Join(t.TempDir(), "user.json"),
		TranscriptOutputDir: "artifacts/transcripts",
		HistoryWindow:       config.DefaultHistoryWindow,
		SyncInterval:        time.Duration(config.DefaultSyncIntervalMS) * time.Millisecond,
		ActionSpacing:       time.Duration(config.DefaultActionSpacingMS) * time.Millisecond,
		RPCSpacing:          time.Duration(config.DefaultRPCSpacingMS) * time.Millisecond,
		PinnedCacheTTL:      time.Duration(config.DefaultPinnedCacheTTLMS) * time.Millisecond,
	}
	if err := os.WriteFile(cfg.SessionPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var out bytes.Buffer
	printDoctor(cfg, &out, func(ctx context.Context) (mtproto.AuthStatus, error) {
		return mtproto.AuthStatus{Authorized: true}, nil
	})

	body := out.String()
	if !strings.Contains(body, "auth_status=authorized\n") {
		t.Fatalf("doctor output missing authorized status:\n%s", body)
	}
	if !strings.Contains(body, "auth_status_detail=Telegram accepted the current session\n") {
		t.Fatalf("doctor output missing authorized detail:\n%s", body)
	}
}

func TestDoctorAuthStatusRequiresReloginWhenSessionExistsButTelegramRejectsIt(t *testing.T) {
	cfg := config.Config{
		AppID:       123,
		AppHash:     "0123456789abcdef0123456789abcdef",
		SessionPath: "/tmp/test-session.json",
	}

	status, detail := doctorAuthStatus(cfg, true, func(ctx context.Context) (mtproto.AuthStatus, error) {
		return mtproto.AuthStatus{Authorized: false}, nil
	})

	if status != "reauth_required" {
		t.Fatalf("status = %q, want reauth_required", status)
	}
	if !strings.Contains(detail, "session file exists at /tmp/test-session.json") {
		t.Fatalf("detail = %q", detail)
	}
	if !strings.Contains(detail, "run `tg-e2e-tool login` again") {
		t.Fatalf("detail = %q", detail)
	}
}

func TestDoctorAuthStatusReportsCheckFailureInline(t *testing.T) {
	cfg := config.Config{
		AppID:       123,
		AppHash:     "0123456789abcdef0123456789abcdef",
		SessionPath: "/tmp/test-session.json",
	}

	status, detail := doctorAuthStatus(cfg, true, func(ctx context.Context) (mtproto.AuthStatus, error) {
		return mtproto.AuthStatus{}, os.ErrPermission
	})

	if status != "check_failed" {
		t.Fatalf("status = %q, want check_failed", status)
	}
	if strings.Contains(detail, "\n") || strings.Contains(detail, "\r") {
		t.Fatalf("detail must be single-line, got %q", detail)
	}
	if !strings.Contains(detail, "permission denied") {
		t.Fatalf("detail = %q", detail)
	}
}
