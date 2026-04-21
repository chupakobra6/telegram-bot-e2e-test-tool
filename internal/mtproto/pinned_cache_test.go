package mtproto

import (
	"testing"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/state"
)

func TestPinnedFromVisible(t *testing.T) {
	visible := []state.VisibleMessage{
		{ID: 10, Text: "older"},
		{ID: 20, Text: "dashboard", Pinned: true},
	}

	pinned := pinnedFromVisible(visible)
	if pinned == nil {
		t.Fatal("expected pinned message from visible history")
	}
	if pinned.MessageID != 20 || pinned.Text != "dashboard" {
		t.Fatalf("unexpected pinned message: %+v", pinned)
	}
}

func TestVisibleHasPinnedRefreshSignal(t *testing.T) {
	if !visibleHasPinnedRefreshSignal([]state.VisibleMessage{{Kind: "service", Text: "[service] message pinned"}}) {
		t.Fatal("expected pin service message to force pinned refresh")
	}
	if visibleHasPinnedRefreshSignal([]state.VisibleMessage{{Kind: "text", Text: "hello"}}) {
		t.Fatal("did not expect plain message to force pinned refresh")
	}
}

func TestPinnedCacheExpires(t *testing.T) {
	s := &Session{
		pinnedTTL:   5 * time.Second,
		pinnedCache: map[int64]pinnedCacheEntry{},
	}
	s.storePinnedCache(42, &state.PinnedMessage{MessageID: 7, Text: "dashboard"})

	if pinned, ok := s.loadPinnedCache(42, time.Now()); !ok || pinned == nil || pinned.MessageID != 7 {
		t.Fatalf("expected fresh pinned cache entry, got pinned=%+v ok=%t", pinned, ok)
	}

	s.pinnedCache[42] = pinnedCacheEntry{
		pinned:    &state.PinnedMessage{MessageID: 7, Text: "dashboard"},
		fetchedAt: time.Now().Add(-s.pinnedTTL - time.Second),
	}
	if pinned, ok := s.loadPinnedCache(42, time.Now()); ok || pinned != nil {
		t.Fatalf("expected expired cache miss, got pinned=%+v ok=%t", pinned, ok)
	}
}
