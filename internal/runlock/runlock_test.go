package runlock

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAcquireBlocksSecondProcess(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "runtime.lock")
	handle, err := Acquire(lockPath)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer func() {
		if err := handle.Release(); err != nil {
			t.Fatalf("Release() error = %v", err)
		}
	}()

	cmd := exec.Command(os.Args[0], "-test.run=TestRunLockHelperProcess", "--", lockPath)
	cmd.Env = append(os.Environ(), "GO_WANT_RUNLOCK_HELPER=1")
	err = cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("helper process should exit with status, err = %v", err)
	}
	if exitErr.ExitCode() != 42 {
		t.Fatalf("helper exit code = %d, want 42", exitErr.ExitCode())
	}
}

func TestAcquireAfterRelease(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "runtime.lock")
	handle, err := Acquire(lockPath)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := handle.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	handle, err = Acquire(lockPath)
	if err != nil {
		t.Fatalf("Acquire() after release error = %v", err)
	}
	if err := handle.Release(); err != nil {
		t.Fatalf("Release() second error = %v", err)
	}
}

func TestRunLockHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_RUNLOCK_HELPER") != "1" {
		return
	}
	lockPath := os.Args[len(os.Args)-1]
	handle, err := Acquire(lockPath)
	if err == nil {
		_ = handle.Release()
		os.Exit(0)
	}
	if errors.Is(err, ErrAlreadyLocked) {
		os.Exit(42)
	}
	os.Exit(43)
}
