package repopolicy

import (
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWorktreeContainsNoPythonOrShellScripts(t *testing.T) {
	root := repoRoot(t)

	var offenders []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if hasScriptExtension(rel) {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir() error = %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("Python/shell files are not allowed in the repo worktree; move tooling into Go instead:\n%s", strings.Join(offenders, "\n"))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

func hasScriptExtension(path string) bool {
	return strings.HasSuffix(path, ".py") || strings.HasSuffix(path, ".sh") || strings.HasSuffix(path, ".bash")
}

func shouldSkipDir(path string) bool {
	switch path {
	case ".git", "artifacts", ".sessions", "tmp":
		return true
	default:
		return false
	}
}
