package triage

import (
	"strings"
	"testing"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/state"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/transcript"
)

func TestBuildCompactTranscriptNormalizesKnownStates(t *testing.T) {
	tr := transcript.New()
	tr.AddCommand(protocol.Command{ID: "start", Action: "send_text", Text: "/start"})
	tr.AddEvent(protocol.Event{
		Type:      "state_update",
		CommandID: "start",
		Action:    "send_text",
		Message:   "visible chat state changed",
		Diff:      &state.ChatDiff{Summary: "added=1"},
		Snapshot: &state.ChatState{
			Pinned: &state.PinnedMessage{
				MessageID: 1,
				Text:      "🧺 Главная\n\nСейчас отслеживаем: 1\nСкоро истекают: 1\nУже истекли: 0\n\nЧтобы добавить продукт, просто отправь текст или голосовое.",
				Buttons:   [][]state.InlineButton{{{Text: "⚙️ Настройки"}}},
			},
		},
	})
	tr.AddEvent(protocol.Event{
		Type:      "state_update",
		CommandID: "start",
		Action:    "send_text",
		Message:   "visible chat state changed",
		Diff:      &state.ChatDiff{Summary: "changed=1"},
		Snapshot: &state.ChatState{
			Pinned: &state.PinnedMessage{
				MessageID: 1,
				Text:      "🧺 Главная\n\nСейчас отслеживаем: 1\nСкоро истекают: 1\nУже истекли: 0\n\nЧтобы добавить продукт, просто отправь текст или голосовое.",
				Buttons:   [][]state.InlineButton{{{Text: "⚙️ Настройки"}}},
			},
		},
	})
	tr.AddEvent(protocol.Event{
		Type:      "state_update",
		CommandID: "draft",
		Action:    "send_text",
		Diff:      &state.ChatDiff{Summary: "added=1"},
		Snapshot: &state.ChatState{
			Pinned: &state.PinnedMessage{
				MessageID: 1,
				Text:      "🧺 Главная\n\nСейчас отслеживаем: 1\nСкоро истекают: 1\nУже истекли: 0\n\nЧтобы добавить продукт, просто отправь текст или голосовое.",
				Buttons:   [][]state.InlineButton{{{Text: "⚙️ Настройки"}}},
			},
			Messages: []state.VisibleMessage{{
				ID:      2,
				Sender:  "bot",
				Text:    "🧾 Новый продукт\n\nНазвание: молоко\nСрок: не указан\nИсточник: текст\n\n• укажи срок годности",
				Buttons: [][]state.InlineButton{
					{{Text: "📝 Название"}, {Text: "📅 Срок"}},
					{{Text: "↩️ Отмена"}, {Text: "✅ Сохранить"}},
				},
			}},
		},
	})

	compact := BuildCompactTranscript(tr)
	if len(compact.Events) != 4 {
		t.Fatalf("expected 4 compact events, got %d", len(compact.Events))
	}
	if compact.Events[1].State != "home tracked=1 soon=1 expired=0" {
		t.Fatalf("unexpected normalized home state: %#v", compact.Events[1])
	}
	if compact.Events[2].State != "" {
		t.Fatalf("expected repeated pinned state to be suppressed, got %#v", compact.Events[2])
	}
	if compact.Events[3].State != "draft name=молоко date=missing source=текст status=needs_date" {
		t.Fatalf("unexpected normalized draft state: %#v", compact.Events[3])
	}
	if compact.FinalStateLabel != "draft name=молоко date=missing source=текст status=needs_date" {
		t.Fatalf("unexpected final state label: %q", compact.FinalStateLabel)
	}
	if got := strings.Join(compact.FinalButtons, ","); got != "edit_name,edit_date,cancel,save,settings" {
		t.Fatalf("unexpected final buttons: %q", got)
	}
}

func TestNormalizeButtonAndLogLines(t *testing.T) {
	if got := normalizeButtonLabel("⚙️ Настройки"); got != "settings" {
		t.Fatalf("normalizeButtonLabel() = %q", got)
	}
	line := `{"time":"2026-04-21T19:16:12Z","level":"INFO","msg":"telegram_send_message_completed","trace_id":"abc","update_id":42,"error":"context deadline exceeded","big":"ignore me"}`
	got := NormalizeLogLine(line)
	for _, want := range []string{"time=2026-04-21T19:16:12Z", "level=INFO", "msg=telegram_send_message_completed", "trace_id=abc", "update_id=42", "error=context deadline exceeded"} {
		if !strings.Contains(got, want) {
			t.Fatalf("NormalizeLogLine() missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "big=") {
		t.Fatalf("NormalizeLogLine() kept unexpected field: %q", got)
	}
}

func TestBuildLastFailureUsesLatestFailedScenario(t *testing.T) {
	now := time.Now().UTC()
	tr := &transcript.Transcript{
		StartedAt: now,
		Events: []transcript.Event{
			{At: now, Kind: "command", Command: &protocol.Command{ID: "select", Action: "select_chat"}},
			{At: now.Add(2 * time.Second), Kind: "event", Output: &protocol.Event{Type: "error", CommandID: "select", Action: "select_chat", Error: "button ⚙️ not found"}},
		},
	}

	rows := []SummaryRow{
		{
			ScenarioPath:    "ok.jsonl",
			TranscriptLabel: "01-ok",
			Status:          "passed",
			StartedAt:       now,
			FinishedAt:      now.Add(time.Second),
			DurationMS:      1000,
			RawJSON:         "/tmp/ok.json",
			RawText:         "/tmp/ok.txt",
			CompactJSON:     "/tmp/ok.compact.json",
			CompactText:     "/tmp/ok.compact.txt",
		},
		{
			ScenarioPath:     "fail.jsonl",
			TranscriptLabel:  "02-fail",
			Status:           "failed",
			StartedAt:        now,
			FinishedAt:       now.Add(2 * time.Second),
			DurationMS:       2000,
			FailingCommandID: "select",
			FailingAction:    "select_chat",
			TerminalError:    "button ⚙️ not found",
			FailureAt:        now.Add(2 * time.Second),
			RawJSON:          "/tmp/fail.json",
			RawText:          "/tmp/fail.txt",
			CompactJSON:      "/tmp/fail.compact.json",
			CompactText:      "/tmp/fail.compact.txt",
		},
	}

	report := BuildLastFailure(rows, map[string]*transcript.Transcript{"02-fail": tr})
	if report == nil {
		t.Fatal("expected failure report")
	}
	if report.ScenarioPath != "fail.jsonl" {
		t.Fatalf("ScenarioPath = %q", report.ScenarioPath)
	}
	if report.FailingCommandID != "select" || report.FailingAction != "select_chat" {
		t.Fatalf("unexpected failing command: %#v", report)
	}
	if len(report.Window) == 0 || report.Window[len(report.Window)-1].Error == "" {
		t.Fatalf("expected failure window with terminal error, got %#v", report.Window)
	}
}
