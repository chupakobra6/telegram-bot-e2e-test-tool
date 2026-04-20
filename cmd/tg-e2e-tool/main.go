package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/config"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/engine"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/mtproto"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/scenario"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/transcript"
)

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
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
		fmt.Printf("app_id=%d\nsession=%s\ntranscripts=%s\ndefault_chat=%s\nhistory_limit=%d\nsync_interval=%s\n",
			cfg.AppID,
			cfg.SessionPath,
			cfg.TranscriptOutputDir,
			cfg.DefaultChat,
			cfg.HistoryLimit,
			cfg.SyncInterval,
		)
	case "doctor":
		printDoctor(cfg, os.Stdout)
	case "login":
		if err := client.Login(context.Background(), os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "interactive":
		tr := transcript.New()
		err := client.RunAuthorized(context.Background(), func(ctx context.Context, session *mtproto.Session) error {
			runner := engine.New(session, tr, os.Stdout, cfg.DefaultChat, cfg.HistoryLimit, cfg.SyncInterval)
			return protocol.ReadCommands(os.Stdin, func(cmd protocol.Command) error {
				return runner.Execute(ctx, cmd)
			})
		})
		saveTranscript(cfg, tr, "interactive", os.Stderr)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "run-scenario":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "run-scenario requires a JSONL path")
			os.Exit(2)
		}
		tr := transcript.New()
		commands, err := scenario.Load(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		prefix := scenarioPrefix(os.Args[2])
		err = client.RunAuthorized(context.Background(), func(ctx context.Context, session *mtproto.Session) error {
			runner := engine.New(session, tr, os.Stdout, cfg.DefaultChat, cfg.HistoryLimit, cfg.SyncInterval)
			for _, cmd := range commands {
				if err := runner.Execute(ctx, cmd); err != nil {
					return err
				}
			}
			return nil
		})
		saveTranscript(cfg, tr, prefix, os.Stderr)
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
	fmt.Fprintln(out, "usage: tg-e2e-tool <help|doctor|login|interactive|run-scenario|print-config> [path]")
}

func printDoctor(cfg config.Config, out *os.File) {
	sessionExists := fileExists(cfg.SessionPath)
	fmt.Fprintf(out, "app_id_set=%t\n", cfg.AppID != 0)
	fmt.Fprintf(out, "app_hash_set=%t\n", strings.TrimSpace(cfg.AppHash) != "")
	fmt.Fprintf(out, "phone_set=%t\n", strings.TrimSpace(cfg.Phone) != "")
	fmt.Fprintf(out, "default_chat=%s\n", emptyAsDash(cfg.DefaultChat))
	fmt.Fprintf(out, "session_path=%s\n", cfg.SessionPath)
	fmt.Fprintf(out, "session_path_mode=%s\n", pathMode(os.Getenv("TG_E2E_SESSION_PATH"), config.DefaultSessionPath))
	fmt.Fprintf(out, "session_exists=%t\n", sessionExists)
	fmt.Fprintf(out, "transcripts=%s\n", cfg.TranscriptOutputDir)
	fmt.Fprintf(out, "transcript_path_mode=%s\n", pathMode(os.Getenv("TG_E2E_TRANSCRIPT_DIR"), config.DefaultTranscriptOutputDir))
	fmt.Fprintf(out, "history_limit=%d\n", cfg.HistoryLimit)
	fmt.Fprintf(out, "sync_interval=%s\n", cfg.SyncInterval)
	fmt.Fprintf(out, "http_proxy_set=%t\n", envSet("HTTP_PROXY"))
	fmt.Fprintf(out, "https_proxy_set=%t\n", envSet("HTTPS_PROXY"))
	fmt.Fprintf(out, "all_proxy_set=%t\n", envSet("ALL_PROXY"))
	fmt.Fprintf(out, "no_proxy_set=%t\n", envSet("NO_PROXY"))
}

func saveTranscript(cfg config.Config, tr *transcript.Transcript, prefix string, stderr *os.File) {
	if err := tr.SaveJSON(filepath.Join(cfg.TranscriptOutputDir, prefix+".json")); err != nil {
		fmt.Fprintf(stderr, "warning: failed to save JSON transcript: %v\n", err)
	}
	if err := tr.SaveText(filepath.Join(cfg.TranscriptOutputDir, prefix+".txt")); err != nil {
		fmt.Fprintf(stderr, "warning: failed to save text transcript: %v\n", err)
	}
}

func scenarioPrefix(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if name == "" {
		return "scenario"
	}
	return name
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
	if strings.TrimSpace(rawValue) == "" {
		return "default(" + defaultPath + ")"
	}
	return "override"
}

func emptyAsDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
