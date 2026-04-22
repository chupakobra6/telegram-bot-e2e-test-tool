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

type scenarioSource struct {
	ScenarioPath string
	Prefix       string
	Read         func(func(protocol.Command) error) error
}

func pathScenarioSource(path, targetChat string, runPrefix *string) (scenarioSource, error) {
	resolved, err := resolveExistingInputPath(path)
	if err != nil {
		return scenarioSource{}, err
	}
	if strings.HasSuffix(resolved, ".tmpl") {
		if runPrefix == nil {
			return scenarioSource{}, fmt.Errorf("templated scenarios are only supported by run-block: %s", path)
		}
		return templatedScenarioSourceFromResolved(resolved, *runPrefix, targetChat)
	}
	return fileScenarioSourceFromResolved(resolved, targetChat), nil
}

func resolveExistingInputPath(path string) (string, error) {
	resolved := resolveInputPath(path)
	if !fileExists(resolved) {
		return "", fmt.Errorf("scenario file not found: %s", path)
	}
	return resolved, nil
}

func fileScenarioSourceFromResolved(resolved, targetChat string) scenarioSource {
	return scenarioSource{
		ScenarioPath: resolved,
		Prefix:       scenarioPrefix(resolved),
		Read: func(fn func(protocol.Command) error) error {
			return scenario.ReadWithOptions(resolved, scenario.ReadOptions{TargetChat: targetChat}, fn)
		},
	}
}

func templatedScenarioSourceFromResolved(resolved, runPrefix, targetChat string) (scenarioSource, error) {
	body, err := os.ReadFile(resolved)
	if err != nil {
		return scenarioSource{}, fmt.Errorf("read template: %w", err)
	}
	rendered := strings.ReplaceAll(string(body), "${RUN_PREFIX}", runPrefix)
	return scenarioSource{
		ScenarioPath: resolved,
		Prefix:       scenarioPrefix(strings.TrimSuffix(resolved, ".tmpl")),
		Read: func(fn func(protocol.Command) error) error {
			return scenario.ReadBytesWithOptions(resolved, []byte(rendered), scenario.ReadOptions{TargetChat: targetChat}, fn)
		},
	}, nil
}

func commandScenarioSource(path, prefix string, commands []protocol.Command) scenarioSource {
	return scenarioSource{
		ScenarioPath: path,
		Prefix:       prefix,
		Read: func(fn func(protocol.Command) error) error {
			for _, cmd := range commands {
				if err := cmd.Validate(); err != nil {
					return err
				}
				if err := fn(cmd); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func runScenarioSources(ctx context.Context, cfg config.Config, session *mtproto.Session, sources []scenarioSource, stdout, stderr *os.File) error {
	artifacts := make([]scenarioArtifacts, 0, len(sources))
	for index, source := range sources {
		tr := transcript.New()
		runner := engine.New(session, tr, stdout, cfg.HistoryWindow, cfg.SyncInterval)
		prefix := source.Prefix
		if prefix == "" {
			prefix = scenarioPrefix(source.ScenarioPath)
		}
		if len(sources) > 1 {
			prefix = fmt.Sprintf("%02d-%s", index+1, prefix)
		}
		if err := runCommandStream(ctx, runner, source.Read); err != nil {
			artifact, saveErr := saveScenarioArtifacts(cfg, tr, source.ScenarioPath, prefix, stderr)
			if saveErr == nil {
				artifacts = append(artifacts, artifact)
				saveLastRunArtifacts(cfg, artifacts, stderr)
			}
			return err
		}
		artifact, _ := saveScenarioArtifacts(cfg, tr, source.ScenarioPath, prefix, stderr)
		artifacts = append(artifacts, artifact)
	}
	saveLastRunArtifacts(cfg, artifacts, stderr)
	return nil
}

func builtInRepoPath(repoRoot, relativePath string) string {
	return filepath.Join(repoRoot, filepath.FromSlash(relativePath))
}
