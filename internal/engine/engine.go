package engine

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/state"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/transcript"
)

type Transport interface {
	SendText(ctx context.Context, chat string, text string) error
	SendPhoto(ctx context.Context, chat string, path string, caption string) error
	SendVoice(ctx context.Context, chat string, path string) error
	SendAudio(ctx context.Context, chat string, path string) error
	ClickButton(ctx context.Context, chat string, messageID int, data []byte) error
	SyncChat(ctx context.Context, chat string, limit int) (state.ChatState, error)
}

type Engine struct {
	transport    Transport
	transcript   *transcript.Transcript
	out          io.Writer
	defaultChat  string
	historyLimit int
	syncInterval time.Duration

	currentChat string
	lastState   *state.ChatState
	pendingWait *protocol.Event
}

func New(transport Transport, tr *transcript.Transcript, out io.Writer, defaultChat string, historyLimit int, syncInterval time.Duration) *Engine {
	return &Engine{
		transport:    transport,
		transcript:   tr,
		out:          out,
		defaultChat:  strings.TrimSpace(defaultChat),
		historyLimit: historyLimit,
		syncInterval: syncInterval,
	}
}

func (e *Engine) Execute(ctx context.Context, cmd protocol.Command) error {
	e.transcript.AddCommand(cmd)
	chat, err := e.resolveChat(cmd)
	if err != nil {
		return e.emitError(cmd, chat, err)
	}

	switch cmd.Action {
	case "send_text":
		if err := e.transport.SendText(ctx, chat, cmd.Text); err != nil {
			return e.emitError(cmd, chat, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat)
	case "send_photo":
		if err := e.transport.SendPhoto(ctx, chat, cmd.Path, cmd.Caption); err != nil {
			return e.emitError(cmd, chat, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat)
	case "send_voice":
		if err := e.transport.SendVoice(ctx, chat, cmd.Path); err != nil {
			return e.emitError(cmd, chat, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat)
	case "send_audio":
		if err := e.transport.SendAudio(ctx, chat, cmd.Path); err != nil {
			return e.emitError(cmd, chat, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat)
	case "click_button":
		if err := e.executeClick(ctx, cmd, chat); err != nil {
			return e.emitError(cmd, chat, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat)
	case "dump_state":
		snapshot, err := e.sync(ctx, chat)
		if err != nil {
			return e.emitError(cmd, chat, err)
		}
		e.lastState = &snapshot
		return e.emit(protocol.Event{
			Type:      "state_snapshot",
			CommandID: cmd.ID,
			Action:    cmd.Action,
			Chat:      chat,
			OK:        true,
			Message:   "chat snapshot captured",
			Snapshot:  &snapshot,
		})
	case "wait":
		return e.executeWait(ctx, cmd, chat)
	default:
		return e.emitError(cmd, chat, fmt.Errorf("unsupported action %q", cmd.Action))
	}
}

func (e *Engine) executeClick(ctx context.Context, cmd protocol.Command, chat string) error {
	snapshot, err := e.sync(ctx, chat)
	if err != nil {
		return err
	}
	messageID, callbackData, err := findButton(snapshot, cmd.ButtonText)
	if err != nil {
		return err
	}
	data, err := base64.StdEncoding.DecodeString(callbackData)
	if err != nil {
		return fmt.Errorf("decode callback data: %w", err)
	}
	return e.transport.ClickButton(ctx, chat, messageID, data)
}

func (e *Engine) executeWait(ctx context.Context, cmd protocol.Command, chat string) error {
	if err := e.emit(protocol.Event{
		Type:      "ack",
		CommandID: cmd.ID,
		Action:    cmd.Action,
		Chat:      chat,
		OK:        true,
		Message:   "waiting for visible chat changes",
	}); err != nil {
		return err
	}

	timeout := time.Duration(cmd.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	if e.pendingWait != nil && e.pendingWait.Chat == chat {
		pending := *e.pendingWait
		pending.CommandID = cmd.ID
		pending.Action = cmd.Action
		e.pendingWait = nil
		return e.emit(pending)
	}

	var baseline state.ChatState
	if e.lastState != nil && e.currentChat == chat {
		baseline = *e.lastState
	} else {
		snapshot, err := e.sync(ctx, chat)
		if err != nil {
			return e.emitError(cmd, chat, err)
		}
		e.lastState = &snapshot
		baseline = snapshot
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(e.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return e.emitError(cmd, chat, ctx.Err())
		case <-deadline.C:
			timeoutErr := fmt.Errorf("wait timeout after %s", timeout)
			if err := e.emit(protocol.Event{
				Type:      "timeout",
				CommandID: cmd.ID,
				Action:    cmd.Action,
				Chat:      chat,
				Error:     timeoutErr.Error(),
				Snapshot:  &baseline,
			}); err != nil {
				return err
			}
			return timeoutErr
		case <-ticker.C:
			snapshot, err := e.transport.SyncChat(ctx, chat, e.historyLimit)
			if err != nil {
				return e.emitError(cmd, chat, err)
			}
			diff := state.Diff(baseline, snapshot)
			if !diff.HasChanges() {
				continue
			}
			e.lastState = &snapshot
			e.currentChat = chat
			e.pendingWait = nil
			return e.emit(protocol.Event{
				Type:      "state_update",
				CommandID: cmd.ID,
				Action:    cmd.Action,
				Chat:      chat,
				OK:        true,
				Message:   "visible chat state changed",
				Snapshot:  &snapshot,
				Diff:      &diff,
			})
		}
	}
}

func (e *Engine) emitAckAndSync(ctx context.Context, cmd protocol.Command, chat string) error {
	if err := e.emit(protocol.Event{
		Type:      "ack",
		CommandID: cmd.ID,
		Action:    cmd.Action,
		Chat:      chat,
		OK:        true,
		Message:   "command executed",
	}); err != nil {
		return err
	}

	snapshot, err := e.sync(ctx, chat)
	if err != nil {
		return e.emitError(cmd, chat, err)
	}
	if e.lastState == nil {
		e.lastState = &snapshot
		event := protocol.Event{
			Type:      "state_snapshot",
			CommandID: cmd.ID,
			Action:    cmd.Action,
			Chat:      chat,
			OK:        true,
			Message:   "chat snapshot captured",
			Snapshot:  &snapshot,
		}
		e.pendingWait = &event
		return e.emit(event)
	}

	diff := state.Diff(*e.lastState, snapshot)
	e.lastState = &snapshot
	if diff.HasChanges() {
		event := protocol.Event{
			Type:      "state_update",
			CommandID: cmd.ID,
			Action:    cmd.Action,
			Chat:      chat,
			OK:        true,
			Message:   "visible chat state changed",
			Snapshot:  &snapshot,
			Diff:      &diff,
		}
		e.pendingWait = &event
		return e.emit(event)
	}
	e.pendingWait = nil
	return e.emit(protocol.Event{
		Type:      "state_snapshot",
		CommandID: cmd.ID,
		Action:    cmd.Action,
		Chat:      chat,
		OK:        true,
		Message:   "visible chat state unchanged",
		Snapshot:  &snapshot,
	})
}

func (e *Engine) sync(ctx context.Context, chat string) (state.ChatState, error) {
	snapshot, err := e.transport.SyncChat(ctx, chat, e.historyLimit)
	if err != nil {
		return state.ChatState{}, err
	}
	e.currentChat = chat
	return snapshot, nil
}

func (e *Engine) resolveChat(cmd protocol.Command) (string, error) {
	if strings.TrimSpace(cmd.Chat) != "" {
		e.currentChat = strings.TrimSpace(cmd.Chat)
		return e.currentChat, nil
	}
	if e.currentChat != "" {
		return e.currentChat, nil
	}
	if e.defaultChat != "" {
		e.currentChat = e.defaultChat
		return e.currentChat, nil
	}
	return "", fmt.Errorf("chat is required for %s", cmd.Action)
}

func (e *Engine) emitError(cmd protocol.Command, chat string, err error) error {
	emitErr := e.emit(protocol.Event{
		Type:      "error",
		CommandID: cmd.ID,
		Action:    cmd.Action,
		Chat:      chat,
		Error:     err.Error(),
	})
	if emitErr != nil {
		return emitErr
	}
	return err
}

func (e *Engine) emit(evt protocol.Event) error {
	e.transcript.AddEvent(evt)
	return protocol.EncodeEvent(e.out, evt)
}

func findButton(snapshot state.ChatState, buttonText string) (int, string, error) {
	msg, ok := latestInteractiveMessage(snapshot)
	if !ok {
		return 0, "", fmt.Errorf("no visible bot message with inline buttons found")
	}
	for _, row := range msg.Buttons {
		for _, button := range row {
			if button.Text != buttonText {
				continue
			}
			if button.Kind != "callback" || button.CallbackData == "" {
				return 0, "", fmt.Errorf("button %q is not a callback button", buttonText)
			}
			return msg.ID, button.CallbackData, nil
		}
	}
	return 0, "", fmt.Errorf("button %q not found in latest interactive message %d", buttonText, msg.ID)
}

func latestInteractiveMessage(snapshot state.ChatState) (state.VisibleMessage, bool) {
	for i := len(snapshot.Messages) - 1; i >= 0; i-- {
		msg := snapshot.Messages[i]
		if len(msg.Buttons) == 0 || msg.Sender != "bot" {
			continue
		}
		return msg, true
	}
	for i := len(snapshot.Messages) - 1; i >= 0; i-- {
		msg := snapshot.Messages[i]
		if len(msg.Buttons) == 0 {
			continue
		}
		return msg, true
	}
	return state.VisibleMessage{}, false
}
