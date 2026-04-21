package main

import (
	"context"
	"encoding/json"
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
	"github.com/igor/telegram-bot-e2e-test-tool/internal/scenario"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/transcript"
)

const lastRunArtifactsFile = "last-run-artifacts.json"

type runScenarioArtifact struct {
	ScenarioPath    string `json:"scenario_path"`
	TranscriptJSON  string `json:"transcript_json"`
	TranscriptText  string `json:"transcript_text"`
	TranscriptLabel string `json:"transcript_label"`
}

type runScenarioArtifactMap struct {
	GeneratedAt time.Time             `json:"generated_at"`
	Entries     []runScenarioArtifact `json:"entries"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}
	if err := config.LoadDotEnv(".env"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	client := mtproto.New(cfg)
	switch os.Args[1] {
	case "help", "--help", "-h":
		printUsage(os.Stdout)
	case "print-config":
		fmt.Printf("app_id=%d\nsession=%s\nruntime_lock=%s\ntranscripts=%s\nhistory_window=%d\nsync_interval=%s\naction_spacing=%s\nrpc_spacing=%s\npinned_cache_ttl=%s\n",
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
	case "doctor":
		printDoctor(cfg, os.Stdout)
	case "login":
		if err := withRuntimeLock(cfg, func() error {
			return client.Login(context.Background(), os.Stdin, os.Stdout)
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "interactive":
		tr := transcript.New()
		err := withRuntimeLock(cfg, func() error {
			return client.RunAuthorized(context.Background(), func(ctx context.Context, session *mtproto.Session) error {
				if err := emitInteractiveReady(tr, os.Stdout, cfg); err != nil {
					return err
				}
				runner := engine.New(session, tr, os.Stdout, cfg.HistoryWindow, cfg.SyncInterval)
				return runCommandStream(ctx, runner, func(fn func(protocol.Command) error) error {
					return protocol.ReadCommands(os.Stdin, fn)
				})
			})
		})
		saveTranscript(cfg, tr, "interactive", os.Stderr)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "run-scenario":
		scenarioPaths := os.Args[2:]
		if len(scenarioPaths) < 1 {
			fmt.Fprintln(os.Stderr, "run-scenario requires at least one JSONL path")
			os.Exit(2)
		}
		targetChat := strings.TrimSpace(os.Getenv("CHAT"))
		err = withRuntimeLock(cfg, func() error {
			return client.RunAuthorized(context.Background(), func(ctx context.Context, session *mtproto.Session) error {
				artifacts := make([]runScenarioArtifact, 0, len(scenarioPaths))
				for index, scenarioPath := range scenarioPaths {
					tr := transcript.New()
					runner := engine.New(session, tr, os.Stdout, cfg.HistoryWindow, cfg.SyncInterval)
					prefix := scenarioPrefixForRun(scenarioPath, index, len(scenarioPaths))
					if err := runCommandStream(ctx, runner, func(fn func(protocol.Command) error) error {
						return scenario.ReadWithOptions(scenarioPath, scenario.ReadOptions{TargetChat: targetChat}, fn)
					}); err != nil {
						artifact, saveErr := saveTranscript(cfg, tr, prefix, os.Stderr)
						if saveErr == nil {
							artifact.ScenarioPath = scenarioPath
							artifacts = append(artifacts, artifact)
							saveLastRunArtifacts(cfg, artifacts, os.Stderr)
						}
						return err
					}
					artifact, _ := saveTranscript(cfg, tr, prefix, os.Stderr)
					artifact.ScenarioPath = scenarioPath
					artifacts = append(artifacts, artifact)
				}
				saveLastRunArtifacts(cfg, artifacts, os.Stderr)
				return nil
			})
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "rate-sweep":
		opts, scenarioPaths, parseErr := parseRateSweepArgs(os.Args[2:])
		if parseErr != nil {
			fmt.Fprintln(os.Stderr, parseErr)
			os.Exit(2)
		}
		opts.ScenarioPaths = scenarioPaths
		err = withRuntimeLock(cfg, func() error {
			return ratesweep.Run(context.Background(), cfg, os.Stdout, opts)
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

func printUsage(out *os.File) {
	fmt.Fprintln(out, "usage: tg-e2e-tool <help|doctor|login|interactive|run-scenario|rate-sweep|print-config> [path ...]")
	fmt.Fprintln(out, "the CLI auto-loads .env from the current working directory when present")
	fmt.Fprintln(out, "rate-sweep options: --chat @bot --runs 3 --artifact-root artifacts/rate-sweep --prepare-scenario path.jsonl --min-action-ms 1800 --max-action-ms 3000 --resolution-ms 100")
}

func printDoctor(cfg config.Config, out *os.File) {
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
	fmt.Fprintf(out, "http_proxy_set=%t\n", envSet("HTTP_PROXY"))
	fmt.Fprintf(out, "https_proxy_set=%t\n", envSet("HTTPS_PROXY"))
	fmt.Fprintf(out, "all_proxy_set=%t\n", envSet("ALL_PROXY"))
	fmt.Fprintf(out, "no_proxy_set=%t\n", envSet("NO_PROXY"))
}

func emitInteractiveReady(tr *transcript.Transcript, out *os.File, cfg config.Config) error {
	evt := protocol.Event{
		Type:    "info",
		OK:      true,
		Message: fmt.Sprintf("interactive session ready; send select_chat first. built-in pacing is active (action_spacing=%s, rpc_spacing=%s, sync_interval=%s)", cfg.ActionSpacing, cfg.RPCSpacing, cfg.SyncInterval),
	}
	tr.AddEvent(evt)
	return protocol.EncodeEvent(out, evt)
}

func runCommandStream(ctx context.Context, runner *engine.Engine, read func(func(protocol.Command) error) error) error {
	return read(func(cmd protocol.Command) error {
		return runner.Execute(ctx, cmd)
	})
}

func withRuntimeLock(cfg config.Config, run func() error) error {
	lock, err := runlock.Acquire(cfg.RuntimeLockPath())
	if err != nil {
		return err
	}
	defer func() {
		_ = lock.Release()
	}()
	return run()
}

func saveTranscript(cfg config.Config, tr *transcript.Transcript, prefix string, stderr *os.File) (runScenarioArtifact, error) {
	jsonPath := filepath.Join(cfg.TranscriptOutputDir, prefix+".json")
	textPath := filepath.Join(cfg.TranscriptOutputDir, prefix+".txt")
	if err := tr.SaveJSON(jsonPath); err != nil {
		fmt.Fprintf(stderr, "warning: failed to save JSON transcript: %v\n", err)
		return runScenarioArtifact{}, err
	}
	if err := tr.SaveText(textPath); err != nil {
		fmt.Fprintf(stderr, "warning: failed to save text transcript: %v\n", err)
		return runScenarioArtifact{}, err
	}
	absJSONPath, err := filepath.Abs(jsonPath)
	if err != nil {
		absJSONPath = jsonPath
	}
	absTextPath, err := filepath.Abs(textPath)
	if err != nil {
		absTextPath = textPath
	}
	return runScenarioArtifact{
		TranscriptJSON:  absJSONPath,
		TranscriptText:  absTextPath,
		TranscriptLabel: prefix,
	}, nil
}

func saveLastRunArtifacts(cfg config.Config, entries []runScenarioArtifact, stderr *os.File) {
	body, err := json.MarshalIndent(runScenarioArtifactMap{
		GeneratedAt: time.Now().UTC(),
		Entries:     entries,
	}, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "warning: failed to encode artifact map: %v\n", err)
		return
	}
	path := filepath.Join(cfg.TranscriptOutputDir, lastRunArtifactsFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(stderr, "warning: failed to create artifact map directory: %v\n", err)
		return
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		fmt.Fprintf(stderr, "warning: failed to save artifact map: %v\n", err)
		return
	}
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
