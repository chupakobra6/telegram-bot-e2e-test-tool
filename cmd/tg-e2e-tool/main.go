package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/config"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/engine"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/mtproto"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/ratesweep"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/runlock"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/transcript"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/triage"
)

const (
	lastRunArtifactsFile   = "last-run-artifacts.json"
	lastRunSummaryJSONFile = "last-run-summary.json"
	lastRunSummaryTextFile = "last-run-summary.txt"
	lastFailureJSONFile    = "last-failure.json"
	lastFailureTextFile    = "last-failure.txt"
)

type runScenarioArtifact struct {
	ScenarioPath          string `json:"scenario_path"`
	TranscriptJSON        string `json:"transcript_json"`
	TranscriptText        string `json:"transcript_text"`
	TranscriptCompactJSON string `json:"transcript_compact_json,omitempty"`
	TranscriptCompactText string `json:"transcript_compact_text,omitempty"`
	TranscriptLabel       string `json:"transcript_label"`
}

type runScenarioArtifactMap struct {
	RunID              string                `json:"run_id"`
	Status             string                `json:"status"`
	Error              string                `json:"error,omitempty"`
	GeneratedAt        time.Time             `json:"generated_at"`
	LastRunSummaryJSON string                `json:"last_run_summary_json,omitempty"`
	LastRunSummaryText string                `json:"last_run_summary_text,omitempty"`
	LastFailureJSON    string                `json:"last_failure_json,omitempty"`
	LastFailureText    string                `json:"last_failure_text,omitempty"`
	Entries            []runScenarioArtifact `json:"entries"`
}

type scenarioArtifacts struct {
	Artifact   runScenarioArtifact
	Summary    triage.SummaryRow
	Transcript *transcript.Transcript
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin, stdout, stderr *os.File) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	repoRoot := detectRepoRoot()
	if err := loadToolDotEnv(repoRoot); err != nil {
		return printError(stderr, 1, err)
	}
	cfg, err := config.Load()
	if err != nil {
		return printError(stderr, 1, err)
	}
	cfg = cfg.WithRoot(repoRoot)
	client := mtproto.New(cfg)
	switch args[0] {
	case "help", "--help", "-h":
		printUsage(stdout)
		return 0
	case "fixtures":
		if err := runFixturesCommand(cfg, repoRoot); err != nil {
			return printError(stderr, 1, err)
		}
	case "print-config":
		fmt.Fprintf(stdout, "app_id=%d\nsession=%s\nruntime_lock=%s\ntranscripts=%s\nhistory_window=%d\nsync_interval=%s\naction_spacing=%s\nrpc_spacing=%s\npinned_cache_ttl=%s\n",
			cfg.AppID,
			cfg.SessionPath,
			cfg.RuntimeLockPath(),
			cfg.TranscriptOutputDir,
			cfg.HistoryWindow,
			cfg.SyncInterval,
			cfg.ActionSpacing,
			cfg.RPCSpacing,
			cfg.PinnedCacheTTL,
		)
		return 0
	case "doctor":
		printDoctor(cfg, stdout, func(ctx context.Context) (mtproto.AuthStatus, error) {
			return client.AuthStatus(ctx)
		})
		return 0
	case "chats":
		err = runChatsCommand(cfg, client, args[1:], stdout)
		if err != nil {
			return printError(stderr, 1, err)
		}
	case "login":
		if err := withRuntimeLock(cfg, func() error {
			return client.Login(context.Background(), stdin, stdout)
		}); err != nil {
			return printError(stderr, 1, err)
		}
	case "clean":
		if err := cleanArtifacts(cfg, repoRoot); err != nil {
			return printError(stderr, 1, err)
		}
	case "interactive":
		err := runRecorded(cfg, []string{"interactive"}, func(result *runResult) error {
			return atStage("authorize", client.RunAuthorized(context.Background(), func(ctx context.Context, session *mtproto.Session) error {
				return result.execute(ctx, session, []scenarioSource{{ScenarioPath: "interactive", Prefix: "interactive", Read: func(fn func(protocol.Command) error) error {
					if err := emitInteractiveReady(transcript.New(), stdout, cfg); err != nil {
						return err
					}
					return protocol.ReadCommands(stdin, fn)
				}}})
			}))
		})
		if err != nil {
			return printError(stderr, 1, err)
		}
	case "run-scenario":
		err = runScenarioCommand(cfg, client, args[1:])
		if err != nil {
			return printError(stderr, 1, err)
		}
	case "run-block":
		err = runBlockCommand(cfg, client, args[1:])
		if err != nil {
			return printError(stderr, 1, err)
		}
	case "run-text-matrix":
		err = runTextMatrixCommand(cfg, client, args[1:])
		if err != nil {
			return printError(stderr, 1, err)
		}
	case "run-suite":
		err = runSuiteCommand(cfg, client, repoRoot)
		if err != nil {
			return printError(stderr, 1, err)
		}
	case "rate-sweep":
		opts, scenarioPaths, parseErr := parseRateSweepArgs(args[1:])
		if parseErr != nil {
			return printError(stderr, 2, parseErr)
		}
		opts.ScenarioPaths = scenarioPaths
		err = withRuntimeLock(cfg, func() error {
			return ratesweep.Run(context.Background(), cfg, stdout, opts)
		})
		if err != nil {
			return printError(stderr, 1, err)
		}
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n\n", args[0])
		printUsage(stderr)
		return 2
	}
	return 0
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "usage: tg-e2e-tool <help|doctor|chats|login|interactive|fixtures|clean|run-scenario|run-block|run-text-matrix|run-suite|rate-sweep|print-config> [path ...]")
	fmt.Fprintln(out, "the CLI auto-loads .env from the current working directory, with repo-root fallback")
	fmt.Fprintln(out, "chats options: --groups --filter TrackMate --topics --admins --json")
	fmt.Fprintln(out, "rate-sweep options: --chat @bot --runs 3 --artifact-root artifacts/rate-sweep --prepare-scenario path.jsonl --min-action-ms 1800 --max-action-ms 3000 --resolution-ms 100")
}

func printError(stderr io.Writer, code int, err error) int {
	fmt.Fprintln(stderr, err)
	return code
}

type authStatusChecker func(context.Context) (mtproto.AuthStatus, error)

func printDoctor(cfg config.Config, out io.Writer, check authStatusChecker) {
	sessionExists := fileExists(cfg.SessionPath)
	fmt.Fprintf(out, "app_id_set=%t\n", cfg.AppID != 0)
	fmt.Fprintf(out, "app_hash_set=%t\n", strings.TrimSpace(cfg.AppHash) != "")
	fmt.Fprintf(out, "phone_set=%t\n", strings.TrimSpace(cfg.Phone) != "")
	fmt.Fprintf(out, "session_path=%s\n", cfg.SessionPath)
	fmt.Fprintf(out, "runtime_lock_path=%s\n", cfg.RuntimeLockPath())
	fmt.Fprintf(out, "session_path_mode=%s\n", pathMode(os.Getenv("TG_E2E_SESSION_PATH"), config.DefaultSessionPath))
	fmt.Fprintf(out, "session_exists=%t\n", sessionExists)
	fmt.Fprintf(out, "transcripts=%s\n", cfg.TranscriptOutputDir)
	fmt.Fprintf(out, "transcript_path_mode=%s\n", pathMode(os.Getenv("TG_E2E_TRANSCRIPT_DIR"), config.DefaultTranscriptOutputDir))
	fmt.Fprintf(out, "history_window=%d (auto)\n", cfg.HistoryWindow)
	fmt.Fprintf(out, "sync_interval=%s\n", cfg.SyncInterval)
	fmt.Fprintf(out, "action_spacing=%s\n", cfg.ActionSpacing)
	fmt.Fprintf(out, "rpc_spacing=%s\n", cfg.RPCSpacing)
	fmt.Fprintf(out, "pinned_cache_ttl=%s\n", cfg.PinnedCacheTTL)
	authStatus, authDetail := doctorAuthStatus(cfg, sessionExists, check)
	fmt.Fprintf(out, "auth_status=%s\n", authStatus)
	if authDetail != "" {
		fmt.Fprintf(out, "auth_status_detail=%s\n", authDetail)
	}
	fmt.Fprintf(out, "http_proxy_set=%t\n", envSet("HTTP_PROXY"))
	fmt.Fprintf(out, "https_proxy_set=%t\n", envSet("HTTPS_PROXY"))
	fmt.Fprintf(out, "all_proxy_set=%t\n", envSet("ALL_PROXY"))
	fmt.Fprintf(out, "no_proxy_set=%t\n", envSet("NO_PROXY"))
}

func doctorAuthStatus(cfg config.Config, sessionExists bool, check authStatusChecker) (string, string) {
	if cfg.AppID == 0 || strings.TrimSpace(cfg.AppHash) == "" {
		return "skipped", "set TG_E2E_APP_ID and TG_E2E_APP_HASH to verify live Telegram authorization"
	}
	if strings.TrimSpace(cfg.SessionPath) == "" {
		return "skipped", "session path is empty"
	}
	if !sessionExists {
		return "reauth_required", "session file is missing; run `tg-e2e-tool login`"
	}
	if check == nil {
		return "check_failed", "no auth checker is configured"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	status, err := check(ctx)
	if err != nil {
		return "check_failed", oneLine(err.Error())
	}
	if status.Authorized {
		return "authorized", "Telegram accepted the current session"
	}
	return "reauth_required", fmt.Sprintf("session file exists at %s, but Telegram requires re-login; run `tg-e2e-tool login` again", cfg.SessionPath)
}

func emitInteractiveReady(tr *transcript.Transcript, out io.Writer, cfg config.Config) error {
	evt := protocol.Event{
		Type:    "info",
		OK:      true,
		Message: fmt.Sprintf("interactive session ready; send select_chat first. built-in pacing is active (action_spacing=%s, rpc_spacing=%s, sync_interval=%s)", cfg.ActionSpacing, cfg.RPCSpacing, cfg.SyncInterval),
	}
	tr.AddEvent(evt)
	return protocol.EncodeEvent(out, evt)
}

func runCommandStream(ctx context.Context, runner *engine.Engine, read func(func(protocol.Command) error) error) error {
	var executionErr error
	readErr := read(func(cmd protocol.Command) error {
		executionErr = runner.Execute(ctx, cmd)
		return executionErr
	})
	if executionErr != nil {
		return atStage("execute", readErr)
	}
	return atStage("read", readErr)
}

func withRuntimeLock(cfg config.Config, run func() error) (resultErr error) {
	lock, err := runlock.Acquire(cfg.RuntimeLockPath())
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, lock.Release())
	}()
	return run()
}

func saveTranscript(cfg config.Config, tr *transcript.Transcript, prefix string, stderr io.Writer) (runScenarioArtifact, error) {
	artifact := runScenarioArtifact{TranscriptLabel: prefix}
	var errs []error
	jsonPath := filepath.Join(cfg.TranscriptOutputDir, prefix+".json")
	textPath := filepath.Join(cfg.TranscriptOutputDir, prefix+".txt")
	if err := tr.SaveJSON(jsonPath); err != nil {
		errs = append(errs, fmt.Errorf("save JSON transcript: %w", err))
	} else {
		artifact.TranscriptJSON = absPathOrFallback(jsonPath)
	}
	if err := tr.SaveText(textPath); err != nil {
		errs = append(errs, fmt.Errorf("save text transcript: %w", err))
	} else {
		artifact.TranscriptText = absPathOrFallback(textPath)
	}
	return artifact, errors.Join(errs...)
}

func saveScenarioArtifacts(cfg config.Config, tr *transcript.Transcript, scenarioPath, prefix string, stderr io.Writer, executionErr error) (scenarioArtifacts, error) {
	artifact, rawErr := saveTranscript(cfg, tr, prefix, stderr)
	artifact.ScenarioPath = scenarioPath
	compact := triage.BuildCompactTranscript(tr)
	compact.RawJSON = artifact.TranscriptJSON
	compactJSONPath := filepath.Join(cfg.TranscriptOutputDir, prefix+".compact.json")
	compactTextPath := filepath.Join(cfg.TranscriptOutputDir, prefix+".compact.txt")
	jsonErr := writeJSONFile(compactJSONPath, compact)
	if jsonErr == nil {
		artifact.TranscriptCompactJSON = absPathOrFallback(compactJSONPath)
	}
	textErr := writeFileAtomic(compactTextPath, []byte(compact.RenderText()))
	if textErr == nil {
		artifact.TranscriptCompactText = absPathOrFallback(compactTextPath)
	}
	saveErr := errors.Join(rawErr, jsonErr, textErr)
	summary := triage.BuildSummaryRow(tr, scenarioPath, prefix, artifact.TranscriptJSON,
		artifact.TranscriptText, artifact.TranscriptCompactJSON, artifact.TranscriptCompactText, executionErr)
	if saveErr != nil {
		summary.Status = "failed"
		summary.FailureStage = "save"
		summary.TerminalError = errors.Join(executionErr, saveErr).Error()
		summary.FailureAt = time.Now().UTC()
	}
	return scenarioArtifacts{Artifact: artifact, Summary: summary, Transcript: tr}, saveErr
}

func saveLastRunArtifacts(cfg config.Config, entries []scenarioArtifacts, stderr io.Writer) error {
	summaryRows := make([]triage.SummaryRow, 0, len(entries))
	artifactRows := make([]runScenarioArtifact, 0, len(entries))
	transcripts := make(map[string]*transcript.Transcript, len(entries))
	runID, status := "", "passed"
	var runErrors []string
	for _, entry := range entries {
		summaryRows = append(summaryRows, entry.Summary)
		artifactRows = append(artifactRows, entry.Artifact)
		if entry.Transcript != nil {
			transcripts[entry.Artifact.TranscriptLabel] = entry.Transcript
		}
		runID = entry.Summary.RunID
		if entry.Summary.RunStatus != "" {
			status = entry.Summary.RunStatus
		}
		if entry.Summary.TerminalError != "" {
			runErrors = append(runErrors, entry.Summary.TerminalError)
		}
	}
	summaryJSONPath := filepath.Join(cfg.TranscriptOutputDir, lastRunSummaryJSONFile)
	summaryTextPath := filepath.Join(cfg.TranscriptOutputDir, lastRunSummaryTextFile)
	failure := triage.BuildLastFailure(summaryRows, transcripts)
	lastFailureJSONPath := filepath.Join(cfg.TranscriptOutputDir, lastFailureJSONFile)
	lastFailureTextPath := filepath.Join(cfg.TranscriptOutputDir, lastFailureTextFile)
	var errs []error
	errs = append(errs, writeJSONFile(summaryJSONPath, summaryRows))
	errs = append(errs, writeFileAtomic(summaryTextPath, []byte(triage.RenderSummaryText(summaryRows))))
	if failure != nil {
		errs = append(errs, writeJSONFile(lastFailureJSONPath, failure))
		errs = append(errs, writeFileAtomic(lastFailureTextPath, []byte(failure.RenderText())))
	} else {
		for _, path := range []string{lastFailureJSONPath, lastFailureTextPath} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("save run summary: %w", err)
	}
	return writeJSONFile(filepath.Join(cfg.TranscriptOutputDir, lastRunArtifactsFile), runScenarioArtifactMap{
		RunID: runID, Status: status, Error: strings.Join(runErrors, "\n"), GeneratedAt: time.Now().UTC(),
		LastRunSummaryJSON: absPathOrFallback(summaryJSONPath), LastRunSummaryText: absPathOrFallback(summaryTextPath),
		LastFailureJSON: optionalAbsPath(lastFailureJSONPath, failure != nil), LastFailureText: optionalAbsPath(lastFailureTextPath, failure != nil),
		Entries: artifactRows,
	})
}

func writeJSONFile(path string, value any) error {
	body, err := triage.MarshalIndent(value)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, body)
}

func writeFileAtomic(path string, body []byte) (resultErr error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".report-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func absPathOrFallback(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func optionalAbsPath(path string, ok bool) string {
	if !ok {
		return ""
	}
	return absPathOrFallback(path)
}

func scenarioPrefix(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if name == "" {
		return "scenario"
	}
	return name
}

func scenarioPrefixForRun(path string, index int, total int) string {
	base := scenarioPrefix(path)
	if total <= 1 {
		return base
	}
	return fmt.Sprintf("%02d-%s", index+1, base)
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func envSet(key string) bool {
	return strings.TrimSpace(os.Getenv(key)) != ""
}

func pathMode(rawValue string, defaultPath string) string {
	trimmed := strings.TrimSpace(rawValue)
	if trimmed == "" || trimmed == defaultPath {
		return "default(" + defaultPath + ")"
	}
	return "override"
}

func oneLine(value string) string {
	var buf bytes.Buffer
	for _, r := range value {
		switch r {
		case '\n', '\r', '\t':
			buf.WriteByte(' ')
		default:
			buf.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

func parseRateSweepArgs(args []string) (ratesweep.Options, []string, error) {
	fs := flag.NewFlagSet("rate-sweep", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		chat             = fs.String("chat", "", "target bot username used for placeholder scenarios")
		runs             = fs.Int("runs", ratesweep.DefaultRuns, "number of repetitions per benchmark scenario")
		artifactRoot     = fs.String("artifact-root", ratesweep.DefaultArtifactRoot, "directory for sweep logs, transcripts, and summary")
		minActionMS      = fs.Int("min-action-ms", ratesweep.DefaultMinActionSpacingMS, "lower action-spacing bound for binary search")
		maxActionMS      = fs.Int("max-action-ms", ratesweep.DefaultMaxActionSpacingMS, "upper action-spacing bound for binary search")
		resolutionMS     = fs.Int("resolution-ms", ratesweep.DefaultResolutionMS, "binary-search resolution in milliseconds")
		prepareScenarios []string
	)
	fs.Func("prepare-scenario", "scenario path executed once before each probe to restore a known baseline", func(value string) error {
		prepareScenarios = append(prepareScenarios, strings.TrimSpace(value))
		return nil
	})
	if err := fs.Parse(args); err != nil {
		return ratesweep.Options{}, nil, fmt.Errorf("parse rate-sweep args: %w", err)
	}

	opts := ratesweep.Options{
		TargetChat:       strings.TrimSpace(*chat),
		PreparePaths:     prepareScenarios,
		Runs:             *runs,
		ArtifactRoot:     strings.TrimSpace(*artifactRoot),
		MinActionSpacing: time.Duration(*minActionMS) * time.Millisecond,
		MaxActionSpacing: time.Duration(*maxActionMS) * time.Millisecond,
		Resolution:       time.Duration(*resolutionMS) * time.Millisecond,
	}
	return opts, fs.Args(), nil
}
