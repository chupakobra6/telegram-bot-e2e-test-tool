package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/config"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/engine"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/transcript"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/triage"
)

type runResult struct {
	id      string
	cfg     config.Config
	entries []scenarioArtifacts
	next    int
}

type stageError struct {
	stage string
	err   error
}

func (e *stageError) Error() string { return e.stage + ": " + e.err.Error() }
func (e *stageError) Unwrap() error { return e.err }
func atStage(stage string, err error) error {
	if err == nil {
		return nil
	}
	var staged *stageError
	if errors.As(err, &staged) {
		return err
	}
	return &stageError{stage: stage, err: err}
}

// The lock covers preparation, authorized execution, cleanup and publication.
// A fresh running summary invalidates the previous run before any external work.
func runRecorded(cfg config.Config, paths []string, body func(*runResult) error) error {
	return withRuntimeLock(cfg, func() error {
		r := &runResult{id: fmt.Sprintf("%s-%d", time.Now().UTC().Format("20060102T150405.000000000Z"), os.Getpid()), cfg: cfg}
		r.cfg.TranscriptOutputDir = filepath.Join(cfg.TranscriptOutputDir, "runs", r.id)
		r.plan(paths)
		if err := writeJSONFile(filepath.Join(cfg.TranscriptOutputDir, lastRunArtifactsFile), runScenarioArtifactMap{RunID: r.id, Status: "running", GeneratedAt: time.Now().UTC()}); err != nil {
			return atStage("initialize result", err)
		}
		runErr := atStage("initialize result", r.publish(cfg, "running"))
		if runErr == nil {
			runErr = body(r)
		}
		if runErr == nil && r.next != len(r.entries) {
			runErr = atStage("execute", fmt.Errorf("only %d of %d scenarios completed", r.next, len(r.entries)))
		}
		status := "passed"
		if runErr != nil {
			status = "failed"
			now := time.Now().UTC()
			stage := "run"
			var staged *stageError
			if errors.As(runErr, &staged) {
				stage = staged.stage
			}
			represented := false
			for _, entry := range r.entries {
				if entry.Summary.Status == "failed" && entry.Summary.TerminalError == runErr.Error() {
					represented = true
				}
			}
			if !represented {
				r.entries = append(r.entries, scenarioArtifacts{Summary: triage.SummaryRow{
					ScenarioPath: "[run]", Status: "failed", ExecutionStatus: "not_run", PropertyStatus: "not_checked",
					FailureStage: stage, TerminalError: runErr.Error(), StartedAt: now, FinishedAt: now, FailureAt: now,
				}})
			}
		}
		saveErr := r.publish(cfg, status)
		if saveErr != nil {
			// Mark any writable summary failed; preserve partial files and their pointers.
			for i := range r.entries {
				r.entries[i].Summary.RunStatus = "failed"
			}
			r.entries = append(r.entries, scenarioArtifacts{Summary: triage.SummaryRow{RunID: r.id, RunStatus: "failed", ScenarioPath: "[run]", Status: "failed", ExecutionStatus: "not_run", PropertyStatus: "not_checked", FailureStage: "save", TerminalError: saveErr.Error(), FailureAt: time.Now().UTC()}})
			_ = r.publish(cfg, "failed")
			artifactRows := make([]runScenarioArtifact, 0, len(r.entries))
			for _, entry := range r.entries {
				artifactRows = append(artifactRows, entry.Artifact)
			}
			// Leave the authoritative manifest failed even if one derived file cannot be saved.
			markerErr := writeJSONFile(filepath.Join(cfg.TranscriptOutputDir, lastRunArtifactsFile), runScenarioArtifactMap{
				RunID: r.id, Status: "failed", Error: errors.Join(runErr, saveErr).Error(), GeneratedAt: time.Now().UTC(), Entries: artifactRows, LastRunSummaryJSON: absPathOrFallback(filepath.Join(r.cfg.TranscriptOutputDir, lastRunSummaryJSONFile)),
			})
			return errors.Join(runErr, atStage("save summary", saveErr), markerErr)
		}
		return runErr
	})
}

func (r *runResult) plan(paths []string) {
	for _, path := range paths {
		r.entries = append(r.entries, scenarioArtifacts{
			Artifact: runScenarioArtifact{ScenarioPath: path},
			Summary:  triage.SummaryRow{ScenarioPath: path, Status: "not_run", ExecutionStatus: "not_run", PropertyStatus: "not_checked"},
		})
	}
}

func (r *runResult) publish(cfg config.Config, status string) error {
	// Even a command with no resolvable inputs has a current run identity.
	if len(r.entries) == 0 {
		r.plan([]string{"[run]"})
	}
	for i := range r.entries {
		r.entries[i].Summary.RunID = r.id
		r.entries[i].Summary.RunStatus = status
	}
	// Keep the full run together, then publish the latest view in the established paths.
	return errors.Join(saveLastRunArtifacts(r.cfg, r.entries, os.Stderr), saveLastRunArtifacts(cfg, r.entries, os.Stderr))
}

func (r *runResult) execute(ctx context.Context, transport engine.Transport, sources []scenarioSource) error {
	for _, source := range sources {
		tr := transcript.New()
		runner := engine.New(transport, tr, os.Stdout, r.cfg.HistoryWindow, r.cfg.SyncInterval)
		executionErr := runCommandStream(ctx, runner, source.Read)
		prefix := fmt.Sprintf("%03d-%s", r.next+1, source.Prefix)
		artifact, saveErr := saveScenarioArtifacts(r.cfg, tr, source.ScenarioPath, prefix, os.Stderr, executionErr)
		if executionErr != nil {
			var staged *stageError
			if errors.As(executionErr, &staged) {
				artifact.Summary.FailureStage = staged.stage
			}
		}
		if saveErr != nil {
			artifact.Summary.FailureStage = "save"
		}
		if r.next < len(r.entries) {
			r.entries[r.next] = artifact
		} else {
			r.entries = append(r.entries, artifact)
		}
		r.next++
		if err := errors.Join(executionErr, saveErr); err != nil {
			return atStage(artifact.Summary.FailureStage, err)
		}
	}
	return nil
}

func cleanArtifacts(cfg config.Config, repoRoot string) error {
	return withRuntimeLock(cfg, func() error {
		var errs []error
		for _, path := range []string{cfg.TranscriptOutputDir, filepath.Join(repoRoot, "artifacts", "fixtures"), filepath.Join(repoRoot, "artifacts", "rate-sweep")} {
			// A custom output must never contain the runtime lock or the persistent session.
			for _, protected := range []string{cfg.RuntimeLockPath(), cfg.SessionPath} {
				rel, err := filepath.Rel(canonicalPath(path), canonicalPath(protected))
				if err != nil {
					return err
				}
				if rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					return fmt.Errorf("refuse to clean %s: contains session or runtime lock", path)
				}
			}
		}
		for _, path := range []string{cfg.TranscriptOutputDir, filepath.Join(repoRoot, "artifacts", "fixtures"), filepath.Join(repoRoot, "artifacts", "rate-sweep")} {
			errs = append(errs, os.RemoveAll(path))
		}
		return errors.Join(errs...)
	})
}

// Resolve existing ancestors too, so a symlinked parent cannot hide a protected file.
func canonicalPath(path string) string {
	path = absPathOrFallback(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(canonicalPath(parent), filepath.Base(path))
}
