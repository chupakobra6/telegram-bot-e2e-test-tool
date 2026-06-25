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
		chat            string
		messageThreadID int
		text            string
	}
	editTextCalls []struct {
		chat            string
		messageThreadID int
		messageID       int
		text            string
	}
	sendPhotoGroupCalls []struct {
		chat            string
		messageThreadID int
		paths           []string
		caption         string
	}
	clickErrs  []error
	clickErr   error
	clickCalls []struct {
		chat            string
		messageThreadID int
		messageID       int
		data            []byte
	}
	deleteCalls []struct {
		chat            string
		messageThreadID int
		messageIDs      []int
	}
	snapshots         []state.ChatState
	syncCallsByTarget []struct {
		chat            string
		messageThreadID int
	}
	syncCalls int
}

func (f *fakeTransport) SendText(_ context.Context, chat string, messageThreadID int, text string) error {
	f.sendTextCalls = append(f.sendTextCalls, struct {
		chat            string
		messageThreadID int
		text            string
	}{chat: chat, messageThreadID: messageThreadID, text: text})
	return nil
}

func (f *fakeTransport) EditText(_ context.Context, chat string, messageThreadID int, messageID int, text string) error {
	f.editTextCalls = append(f.editTextCalls, struct {
		chat            string
		messageThreadID int
		messageID       int
		text            string
	}{chat: chat, messageThreadID: messageThreadID, messageID: messageID, text: text})
	return nil
}

func (f *fakeTransport) SendPhoto(context.Context, string, int, string, string) error { return nil }
func (f *fakeTransport) SendPhotoGroup(_ context.Context, chat string, messageThreadID int, paths []string, caption string) error {
	f.sendPhotoGroupCalls = append(f.sendPhotoGroupCalls, struct {
		chat            string
		messageThreadID int
		paths           []string
		caption         string
	}{chat: chat, messageThreadID: messageThreadID, paths: append([]string(nil), paths...), caption: caption})
	return nil
}
func (f *fakeTransport) SendDocument(context.Context, string, int, string, string) error {
	return nil
}
func (f *fakeTransport) SendVoice(context.Context, string, int, string) error { return nil }
func (f *fakeTransport) SendAudio(context.Context, string, int, string) error { return nil }

func (f *fakeTransport) ClickButton(_ context.Context, chat string, messageThreadID int, messageID int, data []byte) error {
	f.clickCalls = append(f.clickCalls, struct {
		chat            string
		messageThreadID int
		messageID       int
		data            []byte
	}{chat: chat, messageThreadID: messageThreadID, messageID: messageID, data: data})
	if len(f.clickErrs) > 0 {
		err := f.clickErrs[0]
		f.clickErrs = f.clickErrs[1:]
		return err
	}
	return f.clickErr
}

func (f *fakeTransport) DeleteMessages(_ context.Context, chat string, messageThreadID int, messageIDs []int) error {
	f.deleteCalls = append(f.deleteCalls, struct {
		chat            string
		messageThreadID int
		messageIDs      []int
	}{chat: chat, messageThreadID: messageThreadID, messageIDs: append([]int(nil), messageIDs...)})
	return nil
}

func (f *fakeTransport) SyncChat(_ context.Context, chat string, messageThreadID int, _ int) (state.ChatState, error) {
	f.syncCallsByTarget = append(f.syncCallsByTarget, struct {
		chat            string
		messageThreadID int
	}{chat: chat, messageThreadID: messageThreadID})
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

func TestExecuteSleep(t *testing.T) {
	transport := &fakeTransport{}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	startedAt := time.Now()
	if err := engine.Execute(context.Background(), protocol.Command{
		ID:        "pause",
		Action:    "sleep",
		Chat:      "@bot",
		TimeoutMS: 5,
	}); err != nil {
		t.Fatalf("Execute(sleep) error = %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed < 5*time.Millisecond {
		t.Fatalf("sleep elapsed = %s, want at least 5ms", elapsed)
	}
	events := decodeEvents(t, out.Bytes())
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Message != "sleeping for 5ms" || events[1].Message != "sleep completed" {
		t.Fatalf("unexpected sleep events: %+v", events)
	}
}

func TestExecuteEditTextFindsOutgoingMessage(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{
				Target:          "@group",
				MessageThreadID: 10,
				Messages: []state.VisibleMessage{
					{ID: 11, Sender: "bot", Text: "prompt"},
					{ID: 12, Sender: "self", Outgoing: true, Kind: "text", Text: "old report"},
				},
			},
			{
				Target:          "@group",
				MessageThreadID: 10,
				Messages: []state.VisibleMessage{
					{ID: 11, Sender: "bot", Text: "prompt"},
					{ID: 12, Sender: "self", Outgoing: true, Kind: "text", Text: "new report"},
				},
			},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:              "edit",
		Action:          "edit_text",
		Chat:            "@group",
		MessageThreadID: 10,
		MessageText:     "old report",
		Text:            "new report",
	}); err != nil {
		t.Fatalf("edit_text error = %v", err)
	}
	if len(transport.editTextCalls) != 1 {
		t.Fatalf("edit calls = %d, want 1", len(transport.editTextCalls))
	}
	call := transport.editTextCalls[0]
	if call.chat != "@group" || call.messageThreadID != 10 || call.messageID != 12 || call.text != "new report" {
		t.Fatalf("unexpected edit call: %+v", call)
	}
}

func TestExecuteAssertVisibleText(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{{
			Target: "@group",
			Messages: []state.VisibleMessage{
				{ID: 1, Sender: "bot", Text: "новый отчет опубликован"},
			},
		}},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "assert_new",
		Action: "assert_visible_text",
		Chat:   "@group",
		Text:   "новый отчет",
	}); err != nil {
		t.Fatalf("assert_visible_text error = %v", err)
	}
}

func TestExecuteAssertNotVisibleText(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{{
			Target: "@group",
			Messages: []state.VisibleMessage{
				{ID: 1, Sender: "bot", Text: "новый отчет опубликован"},
			},
		}},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "assert_old_gone",
		Action: "assert_not_visible_text",
		Chat:   "@group",
		Text:   "старый отчет",
	}); err != nil {
		t.Fatalf("assert_not_visible_text error = %v", err)
	}
}

func TestExecuteSendPhotoGroup(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{Target: "@group", MessageThreadID: 12},
			{Target: "@group", MessageThreadID: 12},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:              "select",
		Action:          "select_chat",
		Chat:            "@group",
		MessageThreadID: 12,
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}
	if err := engine.Execute(context.Background(), protocol.Command{
		ID:      "album",
		Action:  "send_photo_group",
		Paths:   []string{"/tmp/one.png", "/tmp/two.png"},
		Caption: "общая подпись",
	}); err != nil {
		t.Fatalf("send_photo_group error = %v", err)
	}

	if len(transport.sendPhotoGroupCalls) != 1 {
		t.Fatalf("expected 1 photo group call, got %d", len(transport.sendPhotoGroupCalls))
	}
	got := transport.sendPhotoGroupCalls[0]
	if got.chat != "@group" || got.messageThreadID != 12 || got.caption != "общая подпись" {
		t.Fatalf("unexpected photo group call: %+v", got)
	}
	if len(got.paths) != 2 || got.paths[0] != "/tmp/one.png" || got.paths[1] != "/tmp/two.png" {
		t.Fatalf("unexpected paths: %+v", got.paths)
	}
}

func TestExecuteCarriesSelectedTopicToCommands(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{
				Target:          "@group",
				MessageThreadID: 123,
				Messages:        []state.VisibleMessage{{ID: 1, Sender: "bot", Text: "topic dashboard"}},
			},
			{
				Target:          "@group",
				MessageThreadID: 123,
				Messages:        []state.VisibleMessage{{ID: 1, Sender: "bot", Text: "topic dashboard"}},
			},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:     "select",
		Action: "select_chat",
		Chat:   "@group",
		Topic:  123,
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
	if len(transport.sendTextCalls) != 1 {
		t.Fatalf("expected 1 sendText call, got %d", len(transport.sendTextCalls))
	}
	if got := transport.sendTextCalls[0]; got.chat != "@group" || got.messageThreadID != 123 || got.text != "/start" {
		t.Fatalf("unexpected sendText call: %+v", got)
	}
	for _, call := range transport.syncCallsByTarget {
		if call.chat != "@group" || call.messageThreadID != 123 {
			t.Fatalf("unexpected sync target: %+v", call)
		}
	}

	events := decodeEvents(t, out.Bytes())
	for _, event := range events {
		if event.MessageThreadID != 123 {
			t.Fatalf("event missing topic: %+v", event)
		}
	}
	if events[1].Snapshot == nil || events[1].Snapshot.MessageThreadID != 123 {
		t.Fatalf("snapshot missing topic: %+v", events[1].Snapshot)
	}
}

func TestExecuteUsesSeparateBaselinePerTopic(t *testing.T) {
	transport := &fakeTransport{
		snapshots: []state.ChatState{
			{Target: "@group", MessageThreadID: 10, Messages: []state.VisibleMessage{{ID: 1, Sender: "bot", Text: "topic 10"}}},
			{Target: "@group", MessageThreadID: 20, Messages: []state.VisibleMessage{{ID: 2, Sender: "bot", Text: "topic 20 before"}}},
			{Target: "@group", MessageThreadID: 20, Messages: []state.VisibleMessage{{ID: 2, Sender: "bot", Text: "topic 20 after"}}},
		},
	}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:              "select",
		Action:          "select_chat",
		Chat:            "@group",
		MessageThreadID: 10,
	}); err != nil {
		t.Fatalf("select_chat error = %v", err)
	}
	if err := engine.Execute(context.Background(), protocol.Command{
		ID:              "send",
		Action:          "send_text",
		Chat:            "@group",
		MessageThreadID: 20,
		Text:            "hello topic 20",
	}); err != nil {
		t.Fatalf("send_text error = %v", err)
	}
	if len(transport.syncCallsByTarget) != 3 {
		t.Fatalf("expected select sync plus topic-20 baseline and follow-up sync, got %+v", transport.syncCallsByTarget)
	}
	if transport.syncCallsByTarget[1].messageThreadID != 20 || transport.syncCallsByTarget[2].messageThreadID != 20 {
		t.Fatalf("expected topic 20 to get its own baseline, got %+v", transport.syncCallsByTarget)
	}
	if len(transport.sendTextCalls) != 1 || transport.sendTextCalls[0].messageThreadID != 20 {
		t.Fatalf("unexpected sendText calls: %+v", transport.sendTextCalls)
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

func TestExecuteDeleteVisibleMessagesKeepsTopicStructure(t *testing.T) {
	before := state.ChatState{
		Target:          "@group",
		MessageThreadID: 10,
		Pinned:          &state.PinnedMessage{MessageID: 20, Text: "Сегодня"},
		Messages: []state.VisibleMessage{
			{ID: 10, Sender: "peer", Kind: "text", Text: "topic starter"},
			{ID: 11, Sender: "peer", Kind: "service", Text: "[service] topic created"},
			{ID: 12, Sender: "bot", Kind: "text", Text: "intro"},
			{ID: 20, Sender: "bot", Kind: "text", Text: "Сегодня", Pinned: true},
			{ID: 30, Sender: "bot", Kind: "text", Text: "task card"},
			{ID: 31, Sender: "self", Kind: "text", Text: "report"},
		},
	}
	after := state.ChatState{
		Target:          "@group",
		MessageThreadID: 10,
		Pinned:          &state.PinnedMessage{MessageID: 20, Text: "Сегодня"},
		Messages: []state.VisibleMessage{
			{ID: 10, Sender: "peer", Kind: "text", Text: "topic starter"},
			{ID: 11, Sender: "peer", Kind: "service", Text: "[service] topic created"},
			{ID: 12, Sender: "bot", Kind: "text", Text: "intro"},
			{ID: 20, Sender: "bot", Kind: "text", Text: "Сегодня", Pinned: true},
		},
	}
	transport := &fakeTransport{snapshots: []state.ChatState{before, after, after}}
	var out bytes.Buffer
	engine := New(transport, transcript.New(), &out, 50, time.Millisecond)

	if err := engine.Execute(context.Background(), protocol.Command{
		ID:              "cleanup",
		Action:          "delete_visible_messages",
		Chat:            "@group",
		MessageThreadID: 10,
		KeepOldest:      true,
	}); err != nil {
		t.Fatalf("delete_visible_messages error = %v", err)
	}

	if len(transport.deleteCalls) != 1 {
		t.Fatalf("expected one delete batch, got %d", len(transport.deleteCalls))
	}
	if got := transport.deleteCalls[0].messageIDs; len(got) != 2 || got[0] != 30 || got[1] != 31 {
		t.Fatalf("unexpected deleted message ids: %+v", got)
	}
	events := decodeEvents(t, out.Bytes())
	if len(events) != 2 || events[0].Type != "ack" || events[1].Type != "state_snapshot" {
		t.Fatalf("unexpected event sequence: %+v", events)
	}
	if events[1].Message != "deleted 2 visible messages" {
		t.Fatalf("unexpected cleanup message: %q", events[1].Message)
	}
}

func TestDeletableVisibleMessageIDsDeletesOldestByDefault(t *testing.T) {
	snapshot := state.ChatState{
		Pinned: &state.PinnedMessage{MessageID: 20, Text: "pinned"},
		Messages: []state.VisibleMessage{
			{ID: 10, Kind: "service", Text: "[service] topic created"},
			{ID: 12, Kind: "text", Text: "old test message"},
			{ID: 20, Kind: "text", Text: "pinned", Pinned: true},
			{ID: 21, Kind: "text", Text: "new test message"},
		},
	}

	got := deletableVisibleMessageIDs(snapshot, 10, "", false)
	if len(got) != 2 || got[0] != 12 || got[1] != 21 {
		t.Fatalf("unexpected deletable ids: %+v", got)
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

	messageID, callbackData, err := findButton(snapshot, "Open", 0, "")
	if err != nil {
		t.Fatalf("findButton() error = %v", err)
	}
	if messageID != 3 || callbackData != "bmV3" {
		t.Fatalf("unexpected button resolution: messageID=%d callbackData=%q", messageID, callbackData)
	}
}

func TestFindButtonCanBeScopedByMessageText(t *testing.T) {
	snapshot := state.ChatState{
		Messages: []state.VisibleMessage{
			{
				ID:     1,
				Sender: "bot",
				Text:   "Task A",
				Buttons: [][]state.InlineButton{{
					{Text: "Open", Kind: "callback", CallbackData: "task-a"},
				}},
			},
			{
				ID:     2,
				Sender: "bot",
				Text:   "Task B",
				Buttons: [][]state.InlineButton{{
					{Text: "Open", Kind: "callback", CallbackData: "task-b"},
				}},
			},
		},
	}

	messageID, callbackData, err := findButton(snapshot, "Open", 0, "Task A")
	if err != nil {
		t.Fatalf("findButton() error = %v", err)
	}
	if messageID != 1 || callbackData != "task-a" {
		t.Fatalf("unexpected scoped button resolution: messageID=%d callbackData=%q", messageID, callbackData)
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

	messageID, callbackData, err := findButton(snapshot, "⚙️ Настройки", 0, "")
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

	messageID, callbackData, err := findButton(snapshot, "✅ Сохранить", 0, "")
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

	messageID, callbackData, err := findButton(snapshot, "⚙️ Настройки", 0, "")
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

	messageID, callbackData, err := findButton(snapshot, "📊 Статистика", 1, "")
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
