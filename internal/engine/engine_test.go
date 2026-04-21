package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/state"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/transcript"
)

type fakeTransport struct {
	sendTextCalls []struct {
		chat string
		text string
	}
	clickErrs  []error
	clickErr   error
	clickCalls []struct {
		chat      string
		messageID int
		data      []byte
	}
	snapshots []state.ChatState
	syncCalls int
}

func (f *fakeTransport) SendText(_ context.Context, chat string, text string) error {
	f.sendTextCalls = append(f.sendTextCalls, struct {
		chat string
		text string
	}{chat: chat, text: text})
	return nil
}

func (f *fakeTransport) SendPhoto(context.Context, string, string, string) error { return nil }
func (f *fakeTransport) SendDocument(context.Context, string, string, string) error {
	return nil
}
func (f *fakeTransport) SendVoice(context.Context, string, string) error { return nil }
func (f *fakeTransport) SendAudio(context.Context, string, string) error { return nil }

func (f *fakeTransport) ClickButton(_ context.Context, chat string, messageID int, data []byte) error {
	f.clickCalls = append(f.clickCalls, struct {
		chat      string
		messageID int
		data      []byte
	}{chat: chat, messageID: messageID, data: data})
	if len(f.clickErrs) > 0 {
		err := f.clickErrs[0]
		f.clickErrs = f.clickErrs[1:]
		return err
	}
	return f.clickErr
}

func (f *fakeTransport) SyncChat(_ context.Context, _ string, _ int) (state.ChatState, error) {
	if len(f.snapshots) == 0 {
		return state.ChatState{}, nil
	}
	idx := f.syncCalls
	if idx >= len(f.snapshots) {
		idx = len(f.snapshots) - 1
	}
	f.syncCalls++
	return f.snapshots[idx], nil
}

func TestExecuteSendText(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{ID: 1, Sender: "bot", Text: "dashboard"},
				},
			},
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{ID: 1, Sender: "bot", Text: "dashboard"},
				},
			},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@bot",
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}
	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "start",
		Action: "send_text",
		Text:   "/start",
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(transport.sendTextCalls) != 1 {
		t.Fatalf("expected 1 sendText call, got %d", len(transport.sendTextCalls))
	}
	if transport.sendTextCalls[0].chat != "@bot" || transport.sendTextCalls[0].text != "/start" {
		t.Fatalf("unexpected sendText call: %+v", transport.sendTextCalls[0])
	}

	events := decodeEvents(t, out.Bytes())
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	if events[0].Type != "ack" || events[1].Type != "state_snapshot" || events[2].Type != "ack" || events[3].Type != "state_snapshot" {
		t.Fatalf("unexpected event sequence: %+v", events)
	}
}

func TestExecuteFirstSendTextPrimesBaselineAndWaitConsumesPendingChange(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{Target: "@bot", Messages: []state.VisibleMessage{{ID: 1, Sender: "bot", Text: "before"}}},
			{Target: "@bot", Messages: []state.VisibleMessage{
				{ID: 1, Sender: "bot", Text: "before"},
				{ID: 2, Sender: "self", Text: "/start"},
				{ID: 3, Sender: "bot", Text: "after"},
			}},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@bot",
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "start",
		Action: "send_text",
		Text:   "/start",
	}); err != nil {
		t.Fatalf("send_text error = %v", err)
	}
	syncCallsBeforeWait := transport.syncCalls
	if err := engine.Execute(context.Background(), protocol.Command{
		ID:        "wait",
		Action:    "wait",
		TimeoutMS: 50,
	}); err != nil {
		t.Fatalf("wait error = %v", err)
	}
	if transport.syncCalls != syncCallsBeforeWait {
		t.Fatalf("expected wait to consume pending first-action change without another sync, got %d -> %d", syncCallsBeforeWait, transport.syncCalls)
	}

	events := decodeEvents(t, out.Bytes())
	if events[len(events)-1].Type != "state_update" {
		t.Fatalf("expected pending state_update, got %+v", events[len(events)-1])
	}
	if events[len(events)-1].Diff == nil || len(events[len(events)-1].Diff.Added) != 2 {
		t.Fatalf("expected first-action pending diff to include new visible messages, got %+v", events[len(events)-1].Diff)
	}
}

func TestExecuteWaitDoesNotConsumePureSelfOutgoingPendingChange(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{Target: "@bot", Messages: []state.VisibleMessage{{ID: 1, Sender: "bot", Text: "before"}}},
			{Target: "@bot", Messages: []state.VisibleMessage{
				{ID: 1, Sender: "bot", Text: "before"},
				{ID: 2, Sender: "self", Outgoing: true, Text: "/start"},
			}},
			{Target: "@bot", Messages: []state.VisibleMessage{
				{ID: 1, Sender: "bot", Text: "after"},
				{ID: 2, Sender: "self", Outgoing: true, Text: "/start"},
			}},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@bot",
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}
	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "start",
		Action: "send_text",
		Text:   "/start",
	}); err != nil {
		t.Fatalf("send_text error = %v", err)
	}

	syncCallsBeforeWait := transport.syncCalls
	if err := engine.Execute(context.Background(), protocol.Command{
		ID:        "wait",
		Action:    "wait",
		TimeoutMS: 50,
	}); err != nil {
		t.Fatalf("wait error = %v", err)
	}
	if transport.syncCalls <= syncCallsBeforeWait {
		t.Fatalf("expected wait to keep polling for bot-side changes, got %d -> %d", syncCallsBeforeWait, transport.syncCalls)
	}

	events := decodeEvents(t, out.Bytes())
	if events[len(events)-1].Type != "state_update" {
		t.Fatalf("expected final state_update, got %+v", events[len(events)-1])
	}
	if events[len(events)-1].Diff == nil || len(events[len(events)-1].Diff.Changed) != 1 || events[len(events)-1].Diff.Changed[0] != 1 {
		t.Fatalf("expected wait to report bot-side change, got %+v", events[len(events)-1].Diff)
	}
}

func TestExecuteWaitIgnoresPureSelfOutgoingPollChange(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{Target: "@bot", Messages: []state.VisibleMessage{{ID: 1, Sender: "bot", Text: "before"}}},
			{Target: "@bot", Messages: []state.VisibleMessage{
				{ID: 1, Sender: "bot", Text: "before"},
				{ID: 2, Sender: "self", Outgoing: true, Text: "/start"},
			}},
			{Target: "@bot", Messages: []state.VisibleMessage{
				{ID: 1, Sender: "bot", Text: "before"},
				{ID: 2, Sender: "self", Outgoing: true, Text: "/start"},
			}},
			{Target: "@bot", Messages: []state.VisibleMessage{
				{ID: 1, Sender: "bot", Text: "after"},
				{ID: 2, Sender: "self", Outgoing: true, Text: "/start"},
			}},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@bot",
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}

	syncCallsBeforeWait := transport.syncCalls
	if err := engine.Execute(context.Background(), protocol.Command{
		ID:        "wait",
		Action:    "wait",
		TimeoutMS: 50,
	}); err != nil {
		t.Fatalf("wait error = %v", err)
	}
	if transport.syncCalls <= syncCallsBeforeWait+1 {
		t.Fatalf("expected wait to keep polling past pure self-only sync result, got %d -> %d", syncCallsBeforeWait, transport.syncCalls)
	}

	events := decodeEvents(t, out.Bytes())
	if events[len(events)-1].Type != "state_update" {
		t.Fatalf("expected final state_update, got %+v", events[len(events)-1])
	}
	if events[len(events)-1].Diff == nil || len(events[len(events)-1].Diff.Changed) != 1 || events[len(events)-1].Diff.Changed[0] != 1 {
		t.Fatalf("expected wait to ignore self-only poll diff and report bot-side change, got %+v", events[len(events)-1].Diff)
	}
}

func TestExecuteWaitDetectsVisibleChange(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{Target: "@bot", Messages: []state.VisibleMessage{{ID: 1, Text: "before"}}},
			{Target: "@bot", Messages: []state.VisibleMessage{{ID: 1, Text: "after"}}},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@bot",
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:        "wait",
		Action:    "wait",
		TimeoutMS: 50,
	}); err != nil {
		t.Fatalf("Execute(wait) error = %v", err)
	}

	events := decodeEvents(t, out.Bytes())
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	if events[2].Type != "ack" || events[3].Type != "state_update" {
		t.Fatalf("unexpected event sequence: %+v", events)
	}
	if events[3].Diff == nil || !events[3].Diff.HasChanges() {
		t.Fatalf("expected visible diff, got %+v", events[3].Diff)
	}
}

func TestExecuteWaitIgnoresPureRemovalNoise(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{Target: "@bot", Messages: []state.VisibleMessage{
				{ID: 1, Sender: "bot", Text: "before"},
				{ID: 2, Sender: "bot", Text: "ephemeral"},
			}},
			{Target: "@bot", Messages: []state.VisibleMessage{
				{ID: 1, Sender: "bot", Text: "before"},
			}},
			{Target: "@bot", Messages: []state.VisibleMessage{
				{ID: 1, Sender: "bot", Text: "before"},
				{ID: 3, Sender: "bot", Text: "after"},
			}},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@bot",
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:        "wait",
		Action:    "wait",
		TimeoutMS: 50,
	}); err != nil {
		t.Fatalf("Execute(wait) error = %v", err)
	}

	events := decodeEvents(t, out.Bytes())
	if got := events[len(events)-1]; got.Type != "state_update" {
		t.Fatalf("expected final state_update, got %+v", got)
	} else if got.Diff == nil || len(got.Diff.Added) != 1 || got.Diff.Added[0] != 3 {
		t.Fatalf("expected wait to ignore pure removal and stop on added message, got %+v", got.Diff)
	}
}

func TestExecuteWaitConsumesPendingVisibleChange(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{Target: "@bot", Messages: []state.VisibleMessage{{ID: 1, Sender: "bot", Text: "old"}}},
			{Target: "@bot", Messages: []state.VisibleMessage{{ID: 1, Sender: "bot", Text: "old"}}},
			{Target: "@bot", Messages: []state.VisibleMessage{{ID: 1, Sender: "bot", Text: "new"}}},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@bot",
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "baseline",
		Action: "dump_state",
	}); err != nil {
		t.Fatalf("dump_state error = %v", err)
	}
	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "send",
		Action: "send_text",
		Text:   "hello",
	}); err != nil {
		t.Fatalf("send_text error = %v", err)
	}
	syncCallsBeforeWait := transport.syncCalls
	if err := engine.Execute(context.Background(), protocol.Command{
		ID:        "wait",
		Action:    "wait",
		TimeoutMS: 50,
	}); err != nil {
		t.Fatalf("wait error = %v", err)
	}
	if transport.syncCalls != syncCallsBeforeWait {
		t.Fatalf("expected wait to consume pending change without another sync, got %d -> %d", syncCallsBeforeWait, transport.syncCalls)
	}

	events := decodeEvents(t, out.Bytes())
	if events[len(events)-1].Type != "state_update" {
		t.Fatalf("expected pending state_update, got %+v", events[len(events)-1])
	}
	if events[len(events)-1].Diff == nil || len(events[len(events)-1].Diff.Changed) != 1 || events[len(events)-1].Diff.Changed[0] != 1 {
		t.Fatalf("expected wait to observe follow-up change, got %+v", events[len(events)-1].Diff)
	}
}

func TestExecuteWaitUsesLastKnownStateAsBaselineWhenNoPendingExists(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{Target: "@bot", Messages: []state.VisibleMessage{{ID: 1, Sender: "bot", Text: "before"}}},
			{Target: "@bot", Messages: []state.VisibleMessage{{ID: 1, Sender: "bot", Text: "before"}}},
			{Target: "@bot", Messages: []state.VisibleMessage{{ID: 1, Sender: "bot", Text: "after"}}},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@bot",
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "baseline",
		Action: "dump_state",
	}); err != nil {
		t.Fatalf("dump_state error = %v", err)
	}

	syncCallsBeforeWait := transport.syncCalls
	if err := engine.Execute(context.Background(), protocol.Command{
		ID:        "wait",
		Action:    "wait",
		TimeoutMS: 50,
	}); err != nil {
		t.Fatalf("wait error = %v", err)
	}
	if transport.syncCalls <= syncCallsBeforeWait {
		t.Fatalf("expected wait to perform another sync, got %d -> %d", syncCallsBeforeWait, transport.syncCalls)
	}
}

func TestExecuteSelectChatCapturesSnapshot(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{Target: "@bot", Messages: []state.VisibleMessage{{ID: 1, Sender: "bot", Text: "dashboard"}}},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@bot",
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}

	if transport.syncCalls != 1 {
		t.Fatalf("expected select_chat to sync once, got %d", transport.syncCalls)
	}
	events := decodeEvents(t, out.Bytes())
	if len(events) != 2 || events[0].Type != "ack" || events[1].Type != "state_snapshot" {
		t.Fatalf("unexpected event sequence: %+v", events)
	}
	if events[1].Snapshot == nil || events[1].Snapshot.Target != "@bot" {
		t.Fatalf("unexpected snapshot: %+v", events[1].Snapshot)
	}
}

func TestExecuteWithoutChatRequiresExplicitSelection(t *testing.T) {
	var out bytes.Buffer
	engine := New(&fakeTransport{}, transcript.New(), &out, 50, time.Millisecond)

	err := engine.Execute(context.Background(), protocol.Command{
		ID:     "start",
		Action: "send_text",
		Text:   "/start",
	})
	if err == nil {
		t.Fatal("expected missing chat error")
	}
	if err.Error() != "chat is required for send_text; use select_chat first or pass chat explicitly" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFindButtonUsesLatestVisibleBotMessage(t *testing.T) {
	snapshot := state.ChatState{
		Messages: []state.VisibleMessage{
			{
				ID:     1,
				Sender: "bot",
				Buttons: [][]state.InlineButton{{
					{Text: "Open", Kind: "callback", CallbackData: "b2xk"},
				}},
			},
			{
				ID:     2,
				Sender: "self",
				Buttons: [][]state.InlineButton{{
					{Text: "Open", Kind: "callback", CallbackData: "c2VsZg=="},
				}},
			},
			{
				ID:     3,
				Sender: "bot",
				Buttons: [][]state.InlineButton{{
					{Text: "Open", Kind: "callback", CallbackData: "bmV3"},
				}},
			},
		},
	}

	messageID, callbackData, err := findButton(snapshot, "Open", 0)
	if err != nil {
		t.Fatalf("findButton() error = %v", err)
	}
	if messageID != 3 || callbackData != "bmV3" {
		t.Fatalf("unexpected button resolution: messageID=%d callbackData=%q", messageID, callbackData)
	}
}

func TestFindButtonPrefersPinnedMessageOverVisibleDuplicate(t *testing.T) {
	snapshot := state.ChatState{
		Pinned: &state.PinnedMessage{
			MessageID: 99,
			Text:      "dashboard",
			Buttons: [][]state.InlineButton{{
				{Text: "⚙️ Настройки", Kind: "callback", CallbackData: "pinned-settings"},
			}},
		},
		Messages: []state.VisibleMessage{
			{
				ID:     1,
				Sender: "bot",
				Text:   "dashboard",
				Buttons: [][]state.InlineButton{{
					{Text: "⚙️ Настройки", Kind: "callback", CallbackData: "stale-settings"},
				}},
			},
			{
				ID:     2,
				Sender: "bot",
				Buttons: [][]state.InlineButton{{
					{Text: "Different", Kind: "callback", CallbackData: "bmV3"},
				}},
			},
		},
	}

	messageID, callbackData, err := findButton(snapshot, "⚙️ Настройки", 0)
	if err != nil {
		t.Fatalf("findButton() error = %v", err)
	}
	if messageID != 99 || callbackData != "pinned-settings" {
		t.Fatalf("expected pinned button resolution, got messageID=%d callbackData=%q", messageID, callbackData)
	}
}

func TestFindButtonFallsBackToLatestVisibleNonPinnedMessage(t *testing.T) {
	snapshot := state.ChatState{
		Pinned: &state.PinnedMessage{
			MessageID: 50,
			Text:      "dashboard",
			Buttons: [][]state.InlineButton{{
				{Text: "⚙️ Настройки", Kind: "callback", CallbackData: "settings"},
			}},
		},
		Messages: []state.VisibleMessage{
			{
				ID:     1,
				Sender: "bot",
				Text:   "dashboard",
				Buttons: [][]state.InlineButton{{
					{Text: "⚙️ Настройки", Kind: "callback", CallbackData: "stale-settings"},
				}},
			},
			{
				ID:     2,
				Sender: "bot",
				Text:   "🧾 Новый продукт",
				Buttons: [][]state.InlineButton{{
					{Text: "✅ Сохранить", Kind: "callback", CallbackData: "save"},
				}},
			},
		},
	}

	messageID, callbackData, err := findButton(snapshot, "✅ Сохранить", 0)
	if err != nil {
		t.Fatalf("findButton() error = %v", err)
	}
	if messageID != 2 || callbackData != "save" {
		t.Fatalf("unexpected fallback button resolution: messageID=%d callbackData=%q", messageID, callbackData)
	}
}

func TestFindButtonUsesHiddenPinnedMessage(t *testing.T) {
	snapshot := state.ChatState{
		Pinned: &state.PinnedMessage{
			MessageID: 77,
			Text:      "dashboard",
			Buttons: [][]state.InlineButton{{
				{Text: "⚙️ Настройки", Kind: "callback", CallbackData: "settings"},
			}},
		},
		Messages: []state.VisibleMessage{
			{ID: 10, Sender: "self", Text: "/dashboard"},
		},
	}

	messageID, callbackData, err := findButton(snapshot, "⚙️ Настройки", 0)
	if err != nil {
		t.Fatalf("findButton() error = %v", err)
	}
	if messageID != 77 || callbackData != "settings" {
		t.Fatalf("unexpected hidden pinned resolution: messageID=%d callbackData=%q", messageID, callbackData)
	}
}

func TestFindButtonSupportsMessageOffset(t *testing.T) {
	snapshot := state.ChatState{
		Pinned: &state.PinnedMessage{
			MessageID: 99,
			Text:      "dashboard",
			Buttons: [][]state.InlineButton{{
				{Text: "📊 Статистика", Kind: "callback", CallbackData: "pinned"},
			}},
		},
		Messages: []state.VisibleMessage{
			{
				ID:     1,
				Sender: "bot",
				Text:   "dashboard",
				Buttons: [][]state.InlineButton{{
					{Text: "📊 Статистика", Kind: "callback", CallbackData: "b2xk"},
				}},
			},
			{
				ID:     2,
				Sender: "bot",
				Buttons: [][]state.InlineButton{{
					{Text: "📊 Статистика", Kind: "callback", CallbackData: "bmV3"},
				}},
			},
		},
	}

	messageID, callbackData, err := findButton(snapshot, "📊 Статистика", 1)
	if err != nil {
		t.Fatalf("findButton() error = %v", err)
	}
	if messageID != 1 || callbackData != "b2xk" {
		t.Fatalf("unexpected offset button resolution: messageID=%d callbackData=%q", messageID, callbackData)
	}
}

func TestExecuteClickWaitsForNewInteractiveMessageWithRequestedButton(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{
						ID:     1,
						Sender: "bot",
						Text:   "dashboard",
						Buttons: [][]state.InlineButton{{
							{Text: "📋 Список", Kind: "callback", CallbackData: "bGlzdA=="},
						}},
					},
				},
			},
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{
						ID:     1,
						Sender: "bot",
						Text:   "dashboard",
						Buttons: [][]state.InlineButton{{
							{Text: "📋 Список", Kind: "callback", CallbackData: "bGlzdA=="},
						}},
					},
				},
			},
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{
						ID:     1,
						Sender: "bot",
						Text:   "dashboard",
						Buttons: [][]state.InlineButton{{
							{Text: "📋 Список", Kind: "callback", CallbackData: "bGlzdA=="},
						}},
					},
					{ID: 2, Sender: "self", Outgoing: true, Text: "сметана завтра"},
					{ID: 3, Sender: "bot", Text: "⏳ Принял. Собираю карточку продукта."},
				},
			},
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{
						ID:     1,
						Sender: "bot",
						Text:   "dashboard",
						Buttons: [][]state.InlineButton{{
							{Text: "📋 Список", Kind: "callback", CallbackData: "bGlzdA=="},
						}},
					},
					{ID: 2, Sender: "self", Outgoing: true, Text: "сметана завтра"},
					{ID: 3, Sender: "bot", Text: "⏳ Принял. Собираю карточку продукта."},
					{
						ID:     4,
						Sender: "bot",
						Text:   "🧾 Новый продукт",
						Buttons: [][]state.InlineButton{{
							{Text: "✅ Сохранить", Kind: "callback", CallbackData: "c2F2ZQ=="},
						}},
					},
				},
			},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@bot",
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:         "confirm",
		Action:     "click_button",
		ButtonText: "✅ Сохранить",
		TimeoutMS:  50,
	}); err != nil {
		t.Fatalf("click_button error = %v", err)
	}
	if len(transport.clickCalls) != 1 {
		t.Fatalf("expected 1 click call, got %d", len(transport.clickCalls))
	}
	if transport.clickCalls[0].messageID != 4 || string(transport.clickCalls[0].data) != "save" {
		t.Fatalf("unexpected click call: %+v", transport.clickCalls[0])
	}
}

func TestExecuteClickUsesHiddenPinnedMessage(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{
				Target: "@bot",
				Pinned: &state.PinnedMessage{
					MessageID: 77,
					Text:      "dashboard",
					Buttons: [][]state.InlineButton{{
						{Text: "⚙️ Настройки", Kind: "callback", CallbackData: "c2V0dGluZ3M="},
					}},
				},
				Messages: []state.VisibleMessage{
					{ID: 10, Sender: "self", Outgoing: true, Text: "/dashboard"},
				},
			},
			{
				Target: "@bot",
				Pinned: &state.PinnedMessage{
					MessageID: 77,
					Text:      "dashboard",
					Buttons: [][]state.InlineButton{{
						{Text: "⚙️ Настройки", Kind: "callback", CallbackData: "c2V0dGluZ3M="},
					}},
				},
				Messages: []state.VisibleMessage{
					{ID: 10, Sender: "self", Outgoing: true, Text: "/dashboard"},
				},
			},
			{
				Target: "@bot",
				Pinned: &state.PinnedMessage{
					MessageID: 77,
					Text:      "⚙️ Настройки",
					Buttons: [][]state.InlineButton{{
						{Text: "🕘 На час раньше", Kind: "callback", CallbackData: "earlier"},
					}},
				},
				Messages: []state.VisibleMessage{
					{ID: 10, Sender: "self", Outgoing: true, Text: "/dashboard"},
				},
			},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@bot",
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:         "settings",
		Action:     "click_button",
		ButtonText: "⚙️ Настройки",
		TimeoutMS:  50,
	}); err != nil {
		t.Fatalf("click_button error = %v", err)
	}
	if len(transport.clickCalls) != 1 {
		t.Fatalf("expected 1 click call, got %d", len(transport.clickCalls))
	}
	if transport.clickCalls[0].messageID != 77 || string(transport.clickCalls[0].data) != "settings" {
		t.Fatalf("unexpected click call: %+v", transport.clickCalls[0])
	}
}

func TestExecuteClickTimeoutCanStillSucceedWhenVisibleChangeAppears(t *testing.T) {
	transport := &fakeTransport{
		clickErr: timeoutNetError{},
		snapshots: []state.ChatState{
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{
						ID:     1,
						Sender: "bot",
						Text:   "draft",
						Buttons: [][]state.InlineButton{{
							{Text: "↩️ Отмена", Kind: "callback", CallbackData: "Y2FuY2Vs"},
						}},
					},
				},
			},
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{
						ID:     1,
						Sender: "bot",
						Text:   "draft",
						Buttons: [][]state.InlineButton{{
							{Text: "↩️ Отмена", Kind: "callback", CallbackData: "Y2FuY2Vs"},
						}},
					},
				},
			},
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{ID: 2, Sender: "bot", Text: "🗑️ Отменил добавление."},
				},
			},
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{ID: 2, Sender: "bot", Text: "🗑️ Отменил добавление."},
				},
			},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@bot",
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:         "cancel",
		Action:     "click_button",
		ButtonText: "↩️ Отмена",
		TimeoutMS:  50,
	}); err != nil {
		t.Fatalf("click_button should succeed after delayed visible change, got %v", err)
	}
}

func TestExecuteClickMessageIDInvalidCanStillSucceedWhenVisibleChangeAppears(t *testing.T) {
	transport := &fakeTransport{
		clickErr: errors.New("rpcDoRequest: rpc error code 400: MESSAGE_ID_INVALID"),
		snapshots: []state.ChatState{
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{
						ID:     1,
						Sender: "bot",
						Text:   "draft",
						Buttons: [][]state.InlineButton{{
							{Text: "↩️ Отмена", Kind: "callback", CallbackData: "Y2FuY2Vs"},
						}},
					},
				},
			},
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{
						ID:     1,
						Sender: "bot",
						Text:   "draft",
						Buttons: [][]state.InlineButton{{
							{Text: "↩️ Отмена", Kind: "callback", CallbackData: "Y2FuY2Vs"},
						}},
					},
				},
			},
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{ID: 2, Sender: "bot", Text: "🗑️ Отменил добавление."},
				},
			},
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{ID: 2, Sender: "bot", Text: "🗑️ Отменил добавление."},
				},
			},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@bot",
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:         "cancel",
		Action:     "click_button",
		ButtonText: "↩️ Отмена",
		TimeoutMS:  50,
	}); err != nil {
		t.Fatalf("click_button should treat MESSAGE_ID_INVALID as ambiguous when visible change appears, got %v", err)
	}
}

func TestExecuteClickRetriesTimeoutWithoutVisibleEffect(t *testing.T) {
	transport := &fakeTransport{
		clickErrs: []error{timeoutNetError{}, nil},
		snapshots: []state.ChatState{
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{
						ID:     1,
						Sender: "bot",
						Text:   "dashboard",
						Buttons: [][]state.InlineButton{{
							{Text: "⏰ Скоро", Kind: "callback", CallbackData: "c29vbg=="},
						}},
					},
				},
			},
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{
						ID:     1,
						Sender: "bot",
						Text:   "dashboard",
						Buttons: [][]state.InlineButton{{
							{Text: "⏰ Скоро", Kind: "callback", CallbackData: "c29vbg=="},
						}},
					},
				},
			},
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{
						ID:     1,
						Sender: "bot",
						Text:   "dashboard",
						Buttons: [][]state.InlineButton{{
							{Text: "⏰ Скоро", Kind: "callback", CallbackData: "c29vbg=="},
						}},
					},
				},
			},
			{
				Target: "@bot",
				Messages: []state.VisibleMessage{
					{
						ID:     1,
						Sender: "bot",
						Text:   "soon view",
						Buttons: [][]state.InlineButton{{
							{Text: "⬅️ Назад", Kind: "callback", CallbackData: "YmFjaw=="},
						}},
					},
				},
			},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, 100*time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@bot",
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:         "soon",
		Action:     "click_button",
		ButtonText: "⏰ Скоро",
		TimeoutMS:  20,
	}); err != nil {
		t.Fatalf("click_button should retry timeout without visible effect, got %v", err)
	}
	if len(transport.clickCalls) != 2 {
		t.Fatalf("expected 2 click attempts, got %d", len(transport.clickCalls))
	}
}

func decodeEvents(t *testing.T, body []byte) []protocol.Event {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(body), []byte("\n"))
	events := make([]protocol.Event, 0, len(lines))
	for _, line := range lines {
		var evt protocol.Event
		if err := json.Unmarshal(line, &evt); err != nil {
			t.Fatalf("json.Unmarshal(%q): %v", string(line), err)
		}
		events = append(events, evt)
	}
	return events
}

type timeoutNetError struct{}

func (timeoutNetError) Error() string   { return "timeout" }
func (timeoutNetError) Timeout() bool   { return true }
func (timeoutNetError) Temporary() bool { return true }

var _ net.Error = timeoutNetError{}
