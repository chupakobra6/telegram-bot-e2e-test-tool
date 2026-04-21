package mtproto

import (
	"testing"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/state"
)

func TestPinnedFromVisible(t *testing.T) {
	visible := []state.VisibleMessage{
		{ID: 10, Text: "older"},
		{
			ID:     20,
			Text:   "dashboard",
			Pinned: true,
			Buttons: [][]state.InlineButton{{
				{Text: "⚙️ Настройки", Kind: "callback", CallbackData: "settings"},
			}},
		},
	}

	pinned := pinnedFromVisible(visible)
	if pinned == nil {
		t.Fatal("expected pinned message from visible history")
	}
	if pinned.MessageID != 20 || pinned.Text != "dashboard" {
		t.Fatalf("unexpected pinned message: %+v", pinned)
	}
	if len(pinned.Buttons) != 1 || len(pinned.Buttons[0]) != 1 || pinned.Buttons[0][0].Text != "⚙️ Настройки" {
		t.Fatalf("expected pinned buttons to be preserved, got %+v", pinned.Buttons)
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
	s.storePinnedCache(42, &state.PinnedMessage{
		MessageID: 7,
		Text:      "dashboard",
		Buttons: [][]state.InlineButton{{
			{Text: "⚙️ Настройки", Kind: "callback", CallbackData: "settings"},
		}},
	})

	pinned, ok := s.loadPinnedCache(42, time.Now())
	if !ok || pinned == nil || pinned.MessageID != 7 {
		t.Fatalf("expected fresh pinned cache entry, got pinned=%+v ok=%t", pinned, ok)
	}
	if len(pinned.Buttons) != 1 || pinned.Buttons[0][0].Text != "⚙️ Настройки" {
		t.Fatalf("expected pinned cache buttons to round-trip, got %+v", pinned.Buttons)
	}

	s.pinnedCache[42] = pinnedCacheEntry{
		pinned: &state.PinnedMessage{
			MessageID: 7,
			Text:      "dashboard",
			Buttons: [][]state.InlineButton{{
				{Text: "⚙️ Настройки", Kind: "callback", CallbackData: "settings"},
			}},
		},
		fetchedAt: time.Now().Add(-s.pinnedTTL - time.Second),
	}
	if pinned, ok := s.loadPinnedCache(42, time.Now()); ok || pinned != nil {
		t.Fatalf("expected expired cache miss, got pinned=%+v ok=%t", pinned, ok)
	}
}

func TestClonePinnedMessageClonesButtons(t *testing.T) {
	original := &state.PinnedMessage{
		MessageID: 7,
		Text:      "dashboard",
		Buttons: [][]state.InlineButton{{
			{Text: "⚙️ Настройки", Kind: "callback", CallbackData: "settings"},
		}},
	}

	cloned := clonePinnedMessage(original)
	cloned.Buttons[0][0].Text = "changed"

	if original.Buttons[0][0].Text != "⚙️ Настройки" {
		t.Fatalf("expected deep clone of pinned buttons, got original=%+v cloned=%+v", original.Buttons, cloned.Buttons)
	}
}
