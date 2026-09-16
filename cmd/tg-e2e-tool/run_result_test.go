package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/config"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/engine"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/mtproto"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/runlock"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/state"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/triage"
)

type localAuthorized struct {
	calls int
	err   error
}

func (c *localAuthorized) RunAuthorized(ctx context.Context, fn func(context.Context, *mtproto.Session) error) error {
	c.calls++
	if c.err != nil {
		return c.err
	}
	return fn(ctx, nil) // Sleep-only scenarios need no Telegram transport.
}

type localTransport struct{ engine.Transport }

func (*localTransport) SyncChat(context.Context, string, int, int) (state.ChatState, error) {
	return state.ChatState{Messages: []state.VisibleMessage{{ID: 1, Sender: "bot", Text: "ready"}}}, nil
}
func localConfig(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	return config.Config{SessionPath: filepath.Join(root, "sessions", "user.json"), TranscriptOutputDir: filepath.Join(root, "transcripts"), SyncInterval: time.Millisecond, HistoryWindow: 10}
}
func localScenario(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

const sleepScenario = "{\"id\":\"sleep\",\"action\":\"sleep\",\"chat\":\"@fake\",\"timeout_ms\":1}\n"

func readResult(t *testing.T, cfg config.Config) (runScenarioArtifactMap, []triage.SummaryRow) {
	t.Helper()
	var manifest runScenarioArtifactMap
	var rows []triage.SummaryRow
	body, err := os.ReadFile(filepath.Join(cfg.TranscriptOutputDir, lastRunArtifactsFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(filepath.Join(cfg.TranscriptOutputDir, lastRunSummaryJSONFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatal(err)
	}
	return manifest, rows
}

func TestBlockOwnershipIncludesResetExecutionAndCleanup(t *testing.T) {
	cfg := localConfig(t)
	t.Setenv("CHAT", "@fake")
	path := localScenario(t, "ok.jsonl", sleepScenario)
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		lock, err := runlock.Acquire(cfg.RuntimeLockPath())
		if err == nil {
			lock.Release()
			t.Error("control request outside runtime lock")
		}
		if !errors.Is(err, runlock.ErrAlreadyLocked) {
			t.Errorf("lock error: %v", err)
		}
	}))
	defer server.Close()
	client := &localAuthorized{}
	args := []string{"--control-url", server.URL, "--clear-time", path}
	owner, err := runlock.Acquire(cfg.RuntimeLockPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := runBlockCommand(cfg, client, args); !errors.Is(err, runlock.ErrAlreadyLocked) {
		t.Fatalf("second owner: %v", err)
	}
	if len(requests) != 0 || client.calls != 0 {
		t.Fatal("second owner changed state")
	}
	if err := cleanArtifacts(cfg, t.TempDir()); !errors.Is(err, runlock.ErrAlreadyLocked) {
		t.Fatalf("clean during run: %v", err)
	}
	if _, err := os.Stat(cfg.RuntimeLockPath()); err != nil {
		t.Fatal("clean removed lock", err)
	}
	owner.Release()
	if err := runBlockCommand(cfg, client, args); err != nil {
		t.Fatal(err)
	}
	if strings.Join(requests, ",") != "/control/e2e/reset,/control/time/clear" {
		t.Fatal(requests)
	}
	manifest, rows := readResult(t, cfg)
	if manifest.Status != "passed" || rows[0].ExecutionStatus != "passed" || rows[0].PropertyStatus != "not_checked" {
		t.Fatalf("%+v %+v", manifest, rows)
	}
}

func TestBlockReportsResetAndCleanupErrorsAndInvalidatesOldRun(t *testing.T) {
	for _, failPath := range []string{"/control/e2e/reset", "/control/time/clear"} {
		t.Run(failPath, func(t *testing.T) {
			cfg := localConfig(t)
			t.Setenv("CHAT", "@fake")
			client := &localAuthorized{}
			path := localScenario(t, "ok.jsonl", sleepScenario)
			if err := runScenarioCommand(cfg, client, []string{path}); err != nil {
				t.Fatal(err)
			}
			old, _ := readResult(t, cfg)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == failPath {
					http.Error(w, "failed", 503)
				}
			}))
			defer server.Close()
			err := runBlockCommand(cfg, client, []string{"--control-url", server.URL, "--clear-time", path})
			if err == nil || !strings.Contains(err.Error(), failPath) {
				t.Fatalf("expected %s: %v", failPath, err)
			}
			manifest, rows := readResult(t, cfg)
			if manifest.RunID == old.RunID || manifest.Status != "failed" {
				t.Fatalf("stale/green result: %+v", manifest)
			}
			want := "passed"
			if failPath == "/control/e2e/reset" {
				want = "not_run"
			}
			if rows[0].Status != want {
				t.Fatalf("rows: %+v", rows)
			}
			if rows[len(rows)-1].FailureStage == "" {
				t.Fatal("no failure stage")
			}
		})
	}
}

func TestRunPreservesCompletedScenarioParseFailureAndPendingRemainder(t *testing.T) {
	cfg := localConfig(t)
	client := &localAuthorized{}
	first := localScenario(t, "first.jsonl", sleepScenario)
	bad := localScenario(t, "bad.jsonl", sleepScenario+"{invalid json}\n")
	last := localScenario(t, "last.jsonl", sleepScenario)
	err := runScenarioCommand(cfg, client, []string{first, bad, last})
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("parse error: %v", err)
	}
	manifest, rows := readResult(t, cfg)
	if len(rows) != 3 || rows[0].Status != "passed" || rows[1].Status != "failed" || rows[1].FailureStage != "read" || rows[2].Status != "not_run" {
		t.Fatalf("rows: %+v", rows)
	}
	if !rows[2].StartedAt.IsZero() || !rows[2].FinishedAt.IsZero() {
		t.Fatal("invented pending execution time")
	}
	if rows[1].RunID != manifest.RunID || rows[0].RunStatus != "failed" {
		t.Fatal("unbound result")
	}
	for _, row := range rows[:2] {
		if _, err := os.Stat(row.RawJSON); err != nil {
			t.Fatal(err)
		}
	}
	if err := runScenarioCommand(cfg, client, []string{first}); err != nil {
		t.Fatal(err)
	}
	current, _ := readResult(t, cfg)
	if current.RunID == manifest.RunID {
		t.Fatal("run id reused")
	}
	if _, err := os.Stat(rows[0].RawJSON); err != nil {
		t.Fatal("previous useful result lost", err)
	}
}

func TestRunReportsMissingInputAndAuthorizationFailure(t *testing.T) {
	for _, authError := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "authorization"}[authError], func(t *testing.T) {
			cfg := localConfig(t)
			client := &localAuthorized{}
			path := filepath.Join(t.TempDir(), "missing.jsonl")
			if authError {
				path = localScenario(t, "ok.jsonl", sleepScenario)
				client.err = errors.New("authorization unavailable")
			}
			if err := runScenarioCommand(cfg, client, []string{path}); err == nil {
				t.Fatal("expected failure")
			}
			manifest, rows := readResult(t, cfg)
			if manifest.Status != "failed" || rows[0].Status != "not_run" {
				t.Fatalf("%+v %+v", manifest, rows)
			}
		})
	}
}

func TestRunReportsArtifactAndSummaryWriteFailure(t *testing.T) {
	for _, summaryFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "artifact", true: "summary"}[summaryFailure], func(t *testing.T) {
			cfg := localConfig(t)
			err := runRecorded(cfg, []string{"one", "two"}, func(result *runResult) error {
				if summaryFailure {
					path := filepath.Join(cfg.TranscriptOutputDir, lastRunSummaryTextFile)
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				} else {
					// Fail just text output, retaining the independently written raw JSON.
					if err := os.Mkdir(filepath.Join(result.cfg.TranscriptOutputDir, "001-one.txt"), 0700); err != nil {
						t.Fatal(err)
					}
				}
				return result.execute(context.Background(), &localTransport{}, []scenarioSource{
					commandScenarioSource("one", "one", []protocol.Command{{Action: "assert_visible_text", Chat: "@fake", Text: "ready", TimeoutMS: 50}}),
					commandScenarioSource("two", "two", []protocol.Command{{Action: "sleep", Chat: "@fake", TimeoutMS: 1}}),
				})
			})
			if err == nil {
				t.Fatal("write error returned success")
			}
			manifest, rows := readResult(t, cfg)
			if manifest.Status != "failed" || rows[0].RunStatus != "failed" {
				t.Fatalf("green after write failure: %+v %+v", manifest, rows)
			}
			if rows[0].ExecutionStatus != "passed" || rows[0].PropertyStatus != "assertions_passed" {
				t.Fatalf("execution evidence lost: %+v", rows[0])
			}
			if _, err := os.Stat(rows[0].RawJSON); err != nil {
				t.Fatal("partial JSON lost", err)
			}
			if !summaryFailure && (rows[0].Status != "failed" || rows[1].Status != "not_run" || rows[0].RawText != "") {
				t.Fatalf("bad partial result: %+v", rows)
			}
		})
	}
}

func TestCleanPreservesLockIdentityAndProtectsShortSessionPath(t *testing.T) {
	cfg := localConfig(t)
	root := t.TempDir()
	h, err := runlock.Acquire(cfg.RuntimeLockPath())
	if err != nil {
		t.Fatal(err)
	}
	h.Release()
	before, err := os.Stat(cfg.RuntimeLockPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanArtifacts(cfg, root); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(cfg.RuntimeLockPath())
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("lock inode changed")
	}
	for _, name := range []string{"x", "ab"} {
		cfg.SessionPath = filepath.Join(cfg.TranscriptOutputDir, name)
		if err := os.MkdirAll(cfg.TranscriptOutputDir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfg.SessionPath, []byte("user session"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := cleanArtifacts(cfg, root); err == nil {
			t.Fatal("clean allowed session-containing output")
		}
		if _, err := os.Stat(cfg.SessionPath); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFixturesBlockedThroughCLIAndMakeBeforeAnyWrites(t *testing.T) {
	cfg := localConfig(t)
	fixtureDir := filepath.Join(t.TempDir(), "fixtures")
	t.Setenv("TG_E2E_FIXTURE_DIR", fixtureDir)
	t.Setenv("TG_E2E_SESSION_PATH", cfg.SessionPath)
	t.Setenv("TG_E2E_TRANSCRIPT_DIR", cfg.TranscriptOutputDir)
	t.Setenv("TG_E2E_APP_ID", "0")
	owner, err := runlock.Acquire(cfg.RuntimeLockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Release()
	if err := runFixturesCommand(cfg, t.TempDir()); !errors.Is(err, runlock.ErrAlreadyLocked) {
		t.Fatalf("fixtures did not honor lock: %v", err)
	}
	if code := run([]string{"fixtures"}, os.Stdin, os.Stdout, os.Stderr); code != 1 {
		t.Fatalf("CLI exit=%d", code)
	}
	cmd := exec.Command("make", "-f", filepath.Join(detectRepoRoot(), "Makefile"), "fixtures", "CLI="+os.Args[0]+" -test.run=TestFixturesCLIHelperProcess --")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "GO_WANT_FIXTURES_CLI_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), runlock.ErrAlreadyLocked.Error()) {
		t.Fatalf("make fixtures: %v\n%s", err, output)
	}
	if _, err := os.Stat(fixtureDir); !os.IsNotExist(err) {
		t.Fatalf("fixtures touched before ownership: %v", err)
	}
}

func TestFixturesCLIHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_FIXTURES_CLI_HELPER") != "1" {
		return
	}
	os.Exit(run([]string{"fixtures"}, os.Stdin, os.Stdout, os.Stderr))
}

func TestSuiteFixtureGenerationFailureIsPartOfCurrentRun(t *testing.T) {
	cfg := localConfig(t)
	root := t.TempDir()
	client := &localAuthorized{}
	t.Setenv("CHAT", "@fake")
	t.Setenv("TG_E2E_FIXTURE_DIR", "")
	t.Setenv("PATH", t.TempDir())
	err := runSuiteCommand(cfg, client, root)
	if err == nil || !strings.Contains(err.Error(), "ffmpeg is required") || client.calls != 0 {
		t.Fatalf("err=%v authorization=%d", err, client.calls)
	}
	manifest, rows := readResult(t, cfg)
	if manifest.Status != "failed" || rows[len(rows)-1].FailureStage != "prepare" {
		t.Fatalf("%+v %+v", manifest, rows)
	}
	for _, row := range rows[:len(rows)-1] {
		if row.Status != "not_run" {
			t.Fatalf("scenario ran after generation failure: %+v", row)
		}
	}
	owner, err := runlock.Acquire(cfg.RuntimeLockPath())
	if err != nil {
		t.Fatal("ownership was not released", err)
	}
	defer owner.Release()
}

func TestSuiteReusesCompleteFixtureSetWithoutGenerator(t *testing.T) {
	cfg := localConfig(t)
	root := t.TempDir()
	client := &localAuthorized{err: errors.New("stop before Telegram")}
	t.Setenv("CHAT", "@fake")
	t.Setenv("TG_E2E_FIXTURE_DIR", "")
	t.Setenv("PATH", t.TempDir())
	for _, path := range defaultSuite.requiredFixturePaths(root) {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("existing local fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	err := runSuiteCommand(cfg, client, root)
	if err == nil || !strings.Contains(err.Error(), "stop before Telegram") || client.calls != 1 {
		t.Fatalf("fixture set regenerated: %v; calls=%d", err, client.calls)
	}
	for _, path := range defaultSuite.requiredFixturePaths(root) {
		body, err := os.ReadFile(path)
		if err != nil || string(body) != "existing local fixture" {
			t.Fatalf("fixture changed: %s %v", body, err)
		}
	}
}
