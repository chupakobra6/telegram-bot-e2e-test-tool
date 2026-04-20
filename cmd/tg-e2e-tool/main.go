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
		fmt.Fprintln(os.Stderr, "usage: tg-e2e-tool <login|interactive|run-scenario|print-config> [path]")
		os.Exit(2)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	client := mtproto.New(cfg)
	switch os.Args[1] {
	case "print-config":
		fmt.Printf("app_id=%d\nsession=%s\ntranscripts=%s\ndefault_chat=%s\nhistory_limit=%d\nsync_interval=%s\n",
			cfg.AppID,
			cfg.SessionPath,
			cfg.TranscriptOutputDir,
			cfg.DefaultChat,
			cfg.HistoryLimit,
			cfg.SyncInterval,
		)
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
		fmt.Fprintln(os.Stderr, "unknown command")
		os.Exit(2)
	}
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
