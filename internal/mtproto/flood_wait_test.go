package mtproto

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/tgerr"
)

func TestFloodWaitDelay(t *testing.T) {
	delay, ok := floodWaitDelay(tgerr.New(420, "FLOOD_WAIT_9"))
	if !ok {
		t.Fatal("expected FLOOD_WAIT to be detected")
	}
	if delay != 9*time.Second {
		t.Fatalf("delay = %s, want 9s", delay)
	}

	delay, ok = floodWaitDelay(tgerr.New(420, "FLOOD_WAIT_0"))
	if !ok {
		t.Fatal("expected FLOOD_WAIT_0 to be detected")
	}
	if delay != time.Second {
		t.Fatalf("delay = %s, want 1s fallback", delay)
	}

	if _, ok := floodWaitDelay(errors.New("boom")); ok {
		t.Fatal("did not expect generic error to be treated as FLOOD_WAIT")
	}
}

func TestWithFloodWaitRetrySleepRetries(t *testing.T) {
	ctx := context.Background()
	attempts := 0
	sleeps := 0

	err := withFloodWaitRetrySleep(ctx, func(_ context.Context, delay time.Duration) error {
		sleeps++
		if delay != 2*time.Second {
			t.Fatalf("sleep delay = %s, want 2s", delay)
		}
		return nil
	}, func() error {
		attempts++
		if attempts == 1 {
			return tgerr.New(420, "FLOOD_WAIT_2")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retry returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if sleeps != 1 {
		t.Fatalf("sleeps = %d, want 1", sleeps)
	}
}

func TestWithFloodWaitRetrySleepStopsOnContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := withFloodWaitRetrySleep(ctx, func(ctx context.Context, delay time.Duration) error {
		return sleepContext(ctx, delay)
	}, func() error {
		return tgerr.New(420, "FLOOD_WAIT_1")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestReserveActionSlot(t *testing.T) {
	s := &Session{actionSpacing: 2 * time.Second}
	base := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	if delay := s.reserveActionSlot(base); delay != 0 {
		t.Fatalf("first delay = %s, want 0", delay)
	}
	if delay := s.reserveActionSlot(base.Add(500 * time.Millisecond)); delay != 1500*time.Millisecond {
		t.Fatalf("second delay = %s, want 1.5s", delay)
	}
	if delay := s.reserveActionSlot(base.Add(3 * time.Second)); delay != 1*time.Second {
		t.Fatalf("third delay = %s, want 1s", delay)
	}
}

func TestReserveRPCSlot(t *testing.T) {
	s := &Session{rpcSpacing: 800 * time.Millisecond}
	base := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	if delay := s.reserveRPCSlot(base); delay != 0 {
		t.Fatalf("first delay = %s, want 0", delay)
	}
	if delay := s.reserveRPCSlot(base.Add(200 * time.Millisecond)); delay != 600*time.Millisecond {
		t.Fatalf("second delay = %s, want 600ms", delay)
	}
	if delay := s.reserveRPCSlot(base.Add(1500 * time.Millisecond)); delay != 100*time.Millisecond {
		t.Fatalf("third delay = %s, want 100ms", delay)
	}
}

func TestNoteFloodWaitPushesFutureSlots(t *testing.T) {
	s := &Session{
		actionSpacing: 3 * time.Second,
		rpcSpacing:    800 * time.Millisecond,
	}

	s.noteFloodWait("click_button", 5*time.Second)

	now := time.Now()
	if remaining := s.nextRPCAt.Sub(now); remaining < 5*time.Second {
		t.Fatalf("rpc cooldown = %s, want at least 5s", remaining)
	}
	if remaining := s.nextActionAt.Sub(now); remaining < 7*time.Second {
		t.Fatalf("action cooldown = %s, want at least 7s", remaining)
	}
	if stats := s.Stats(); stats.FloodWaits != 1 {
		t.Fatalf("FloodWaits = %d, want 1", stats.FloodWaits)
	}
	events := s.FloodEvents()
	if len(events) != 1 {
		t.Fatalf("len(FloodEvents) = %d, want 1", len(events))
	}
	if events[0].Operation != "click_button" || events[0].Kind != "flood_wait" {
		t.Fatalf("unexpected flood event: %+v", events[0])
	}
}

func TestIsTransportFlood(t *testing.T) {
	if !isTransportFlood(errors.New("rpc failed: transport flood")) {
		t.Fatal("expected transport flood to be detected")
	}
	if isTransportFlood(errors.New("boom")) {
		t.Fatal("did not expect generic error to be treated as transport flood")
	}
}

func TestIsBotResponseTimeout(t *testing.T) {
	if !isBotResponseTimeout(tgerr.New(400, "BOT_RESPONSE_TIMEOUT")) {
		t.Fatal("expected BOT_RESPONSE_TIMEOUT to be detected")
	}
	if isBotResponseTimeout(tgerr.New(400, "MESSAGE_NOT_MODIFIED")) {
		t.Fatal("did not expect unrelated rpc error to match")
	}
}

func TestIsIgnorableClickButtonError(t *testing.T) {
	if !isIgnorableClickButtonError(tgerr.New(400, "BOT_RESPONSE_TIMEOUT")) {
		t.Fatal("expected BOT_RESPONSE_TIMEOUT to be ignored for click_button")
	}
	if isIgnorableClickButtonError(context.DeadlineExceeded) {
		t.Fatal("did not expect context deadline to be ignored for click_button")
	}
	if isIgnorableClickButtonError(timeoutNetError{}) {
		t.Fatal("did not expect net timeout to be ignored for click_button")
	}
	if isIgnorableClickButtonError(errors.New("rpcDoRequest: write tcp 1.2.3.4:443: i/o timeout")) {
		t.Fatal("did not expect wrapped i/o timeout string to be ignored for click_button")
	}
	if isIgnorableClickButtonError(errors.New("boom")) {
		t.Fatal("did not expect generic error to be ignored")
	}
}

type timeoutNetError struct{}

func (timeoutNetError) Error() string   { return "timeout" }
func (timeoutNetError) Timeout() bool   { return true }
func (timeoutNetError) Temporary() bool { return true }

var _ net.Error = timeoutNetError{}

func TestNoteTransportFloodRecordsEvent(t *testing.T) {
	s := &Session{}
	s.noteTransportFlood("sync_history", errors.New("transport flood"))

	stats := s.Stats()
	if stats.TransportFloods != 1 {
		t.Fatalf("TransportFloods = %d, want 1", stats.TransportFloods)
	}
	events := s.FloodEvents()
	if len(events) != 1 {
		t.Fatalf("len(FloodEvents) = %d, want 1", len(events))
	}
	if events[0].Operation != "sync_history" || events[0].Kind != "transport_flood" {
		t.Fatalf("unexpected flood event: %+v", events[0])
	}
}

func TestRPCTimeoutForOperation(t *testing.T) {
	tests := []struct {
		operation string
		want      time.Duration
	}{
		{operation: "send_text", want: defaultRPCTimeout},
		{operation: "click_button", want: defaultClickRPCTimeout},
		{operation: "send_photo", want: defaultMediaRPCTimeout},
		{operation: "send_audio", want: defaultMediaRPCTimeout},
		{operation: "get_dialogs", want: defaultDialogRPCTimeout},
	}

	for _, tt := range tests {
		if got := rpcTimeoutForOperation(tt.operation); got != tt.want {
			t.Fatalf("rpcTimeoutForOperation(%q) = %s, want %s", tt.operation, got, tt.want)
		}
	}
}

func TestUnauthorizedRuntimeErrorMentionsExistingSessionFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := unauthorizedRuntimeError(path)
	if err == nil {
		t.Fatal("expected unauthorized runtime error")
	}
	if !strings.Contains(err.Error(), "session file exists at "+path) {
		t.Fatalf("error = %q", err.Error())
	}
	if !strings.Contains(err.Error(), "requires re-login") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestUnauthorizedRuntimeErrorWithoutSessionFile(t *testing.T) {
	err := unauthorizedRuntimeError(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected unauthorized runtime error")
	}
	if !strings.Contains(err.Error(), "no valid Telegram session is available") {
		t.Fatalf("error = %q", err.Error())
	}
}
