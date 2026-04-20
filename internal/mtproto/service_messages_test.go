package mtproto

import (
	"testing"

	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/tg"
)

func TestNormalizeServiceMessagePin(t *testing.T) {
	msg := tg.MessageService{
		ID:     42,
		Date:   1_713_654_321,
		Action: &tg.MessageActionPinMessage{},
	}

	got := normalizeServiceMessage(msg, peer.Entities{})
	if got.Kind != "service" {
		t.Fatalf("kind = %q, want service", got.Kind)
	}
	if got.Text != "[service] message pinned" {
		t.Fatalf("text = %q", got.Text)
	}
	if got.ID != 42 {
		t.Fatalf("id = %d", got.ID)
	}
}

func TestServiceActionTextFallback(t *testing.T) {
	got := serviceActionText(&tg.MessageActionHistoryClear{})
	if got != "[service] messageActionHistoryClear" {
		t.Fatalf("fallback text = %q", got)
	}
}
