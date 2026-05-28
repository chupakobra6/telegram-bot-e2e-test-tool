package mtproto

import (
	"context"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
)

func TestPinnedSearchRequestSetsTopMsgIDForTopic(t *testing.T) {
	target := resolvedTarget{InputPeer: &tg.InputPeerSelf{}}

	req := pinnedSearchRequest(target, 123)
	if got, ok := req.GetTopMsgID(); !ok || got != 123 {
		t.Fatalf("TopMsgID = %d ok=%t, want 123 true", got, ok)
	}

	req = pinnedSearchRequest(target, 0)
	if got, ok := req.GetTopMsgID(); ok || got != 0 {
		t.Fatalf("TopMsgID = %d ok=%t, want 0 false", got, ok)
	}
}

func TestTopicRepliesRequestUsesMessageThreadID(t *testing.T) {
	target := resolvedTarget{InputPeer: &tg.InputPeerSelf{}}

	req := topicRepliesRequest(target, 123, 50)
	if req.MsgID != 123 {
		t.Fatalf("MsgID = %d, want 123", req.MsgID)
	}
	if req.Limit != 50 {
		t.Fatalf("Limit = %d, want 50", req.Limit)
	}
}

func TestSendTextInTopicUsesReplyToThreadStarter(t *testing.T) {
	mock := newTestInvoker(t)
	raw := tg.NewClient(mock)
	session := topicTestSession(raw)

	mock.expect(func(b bin.Encoder) {
		req, ok := b.(*tg.MessagesSendMessageRequest)
		if !ok {
			t.Fatalf("request type = %T, want *tg.MessagesSendMessageRequest", b)
		}
		reply, ok := req.ReplyTo.(*tg.InputReplyToMessage)
		if !ok {
			t.Fatalf("ReplyTo type = %T, want *tg.InputReplyToMessage", req.ReplyTo)
		}
		if reply.ReplyToMsgID != 123 {
			t.Fatalf("ReplyToMsgID = %d, want 123", reply.ReplyToMsgID)
		}
		if req.Message != "hello" {
			t.Fatalf("Message = %q, want hello", req.Message)
		}
	}, &tg.Updates{})

	if err := session.SendText(context.Background(), "@group", 123, "hello"); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}
	mock.done()
}

func TestSendTextWithoutTopicLeavesReplyToEmpty(t *testing.T) {
	mock := newTestInvoker(t)
	raw := tg.NewClient(mock)
	session := topicTestSession(raw)

	mock.expect(func(b bin.Encoder) {
		req, ok := b.(*tg.MessagesSendMessageRequest)
		if !ok {
			t.Fatalf("request type = %T, want *tg.MessagesSendMessageRequest", b)
		}
		if req.ReplyTo != nil {
			t.Fatalf("ReplyTo = %+v, want nil", req.ReplyTo)
		}
	}, &tg.Updates{})

	if err := session.SendText(context.Background(), "@group", 0, "hello"); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}
	mock.done()
}

func TestSyncChatInTopicUsesRepliesAndPinnedSearchTopMsgID(t *testing.T) {
	mock := newTestInvoker(t)
	raw := tg.NewClient(mock)
	session := topicTestSession(raw)

	mock.expect(func(b bin.Encoder) {
		req, ok := b.(*tg.MessagesGetRepliesRequest)
		if !ok {
			t.Fatalf("request type = %T, want *tg.MessagesGetRepliesRequest", b)
		}
		if req.MsgID != 123 || req.Limit != 50 {
			t.Fatalf("unexpected replies request: %+v", req)
		}
	}, &tg.MessagesMessages{
		Messages: []tg.MessageClass{
			&tg.Message{ID: 2, Date: 1, Message: "topic msg"},
		},
	})
	mock.expect(func(b bin.Encoder) {
		req, ok := b.(*tg.MessagesSearchRequest)
		if !ok {
			t.Fatalf("request type = %T, want *tg.MessagesSearchRequest", b)
		}
		if got, ok := req.GetTopMsgID(); !ok || got != 123 {
			t.Fatalf("TopMsgID = %d ok=%t, want 123 true", got, ok)
		}
	}, &tg.MessagesMessages{})

	snapshot, err := session.SyncChat(context.Background(), "@group", 123, 50)
	if err != nil {
		t.Fatalf("SyncChat() error = %v", err)
	}
	if snapshot.MessageThreadID != 123 {
		t.Fatalf("MessageThreadID = %d, want 123", snapshot.MessageThreadID)
	}
	if len(snapshot.Messages) != 1 || snapshot.Messages[0].ID != 2 {
		t.Fatalf("unexpected messages: %+v", snapshot.Messages)
	}
	mock.done()
}

func topicTestSession(raw *tg.Client) *Session {
	return &Session{
		raw:    raw,
		sender: message.NewSender(raw),
		cache: map[string]resolvedTarget{
			"@group": {
				Raw:       "@group",
				Display:   "@group",
				PeerID:    42,
				InputPeer: &tg.InputPeerSelf{},
			},
		},
		pinnedCache: map[pinnedCacheKey]pinnedCacheEntry{},
	}
}

type testInvoker struct {
	t            *testing.T
	expectations []invokeExpectation
}

type invokeExpectation struct {
	check  func(bin.Encoder)
	result any
}

func newTestInvoker(t *testing.T) *testInvoker {
	t.Helper()
	return &testInvoker{t: t}
}

func (i *testInvoker) expect(check func(bin.Encoder), result any) {
	i.expectations = append(i.expectations, invokeExpectation{check: check, result: result})
}

func (i *testInvoker) Invoke(_ context.Context, input bin.Encoder, output bin.Decoder) error {
	i.t.Helper()
	if len(i.expectations) == 0 {
		i.t.Fatalf("unexpected request: %T", input)
	}
	expectation := i.expectations[0]
	i.expectations = i.expectations[1:]
	expectation.check(input)

	switch out := output.(type) {
	case *tg.UpdatesBox:
		result, ok := expectation.result.(tg.UpdatesClass)
		if !ok {
			i.t.Fatalf("result type = %T, want tg.UpdatesClass", expectation.result)
		}
		out.Updates = result
	case *tg.MessagesMessagesBox:
		result, ok := expectation.result.(tg.MessagesMessagesClass)
		if !ok {
			i.t.Fatalf("result type = %T, want tg.MessagesMessagesClass", expectation.result)
		}
		out.Messages = result
	default:
		i.t.Fatalf("output type = %T", output)
	}
	return nil
}

func (i *testInvoker) done() {
	i.t.Helper()
	if len(i.expectations) != 0 {
		i.t.Fatalf("unused expectations: %d", len(i.expectations))
	}
}
