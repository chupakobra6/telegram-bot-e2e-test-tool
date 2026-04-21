package engine

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/state"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/transcript"
)

type Transport interface {
	SendText(ctx context.Context, chat string, text string) error
	SendPhoto(ctx context.Context, chat string, path string, caption string) error
	SendDocument(ctx context.Context, chat string, path string, caption string) error
	SendVoice(ctx context.Context, chat string, path string) error
	SendAudio(ctx context.Context, chat string, path string) error
	ClickButton(ctx context.Context, chat string, messageID int, data []byte) error
	SyncChat(ctx context.Context, chat string, limit int) (state.ChatState, error)
}

type Engine struct {
	transport     Transport
	transcript    *transcript.Transcript
	out           io.Writer
	historyWindow int
	syncInterval  time.Duration

	currentChat string
	lastState   *state.ChatState
	pendingWait *pendingVisibleChange
}

type pendingVisibleChange struct {
	chat     string
	snapshot state.ChatState
	diff     state.ChatDiff
}

func New(transport Transport, tr *transcript.Transcript, out io.Writer, historyWindow int, syncInterval time.Duration) *Engine {
	return &Engine{
		transport:     transport,
		transcript:    tr,
		out:           out,
		historyWindow: historyWindow,
		syncInterval:  syncInterval,
	}
}

func (e *Engine) Execute(ctx context.Context, cmd protocol.Command) error {
	e.transcript.AddCommand(cmd)
	if cmd.Action != "wait" {
		e.pendingWait = nil
	}
	chat, err := e.resolveChat(cmd)
	if err != nil {
		return e.emitError(cmd, chat, err)
	}

	switch cmd.Action {
	case "select_chat":
		return e.selectChat(ctx, cmd, chat)
	case "send_text":
		if err := e.prepareActionBaseline(ctx, chat); err != nil {
			return e.emitError(cmd, chat, err)
		}
		if err := e.transport.SendText(ctx, chat, cmd.Text); err != nil {
			return e.emitError(cmd, chat, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat)
	case "send_photo":
		if err := e.prepareActionBaseline(ctx, chat); err != nil {
			return e.emitError(cmd, chat, err)
		}
		if err := e.transport.SendPhoto(ctx, chat, cmd.Path, cmd.Caption); err != nil {
			return e.emitError(cmd, chat, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat)
	case "send_document":
		if err := e.prepareActionBaseline(ctx, chat); err != nil {
			return e.emitError(cmd, chat, err)
		}
		if err := e.transport.SendDocument(ctx, chat, cmd.Path, cmd.Caption); err != nil {
			return e.emitError(cmd, chat, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat)
	case "send_voice":
		if err := e.prepareActionBaseline(ctx, chat); err != nil {
			return e.emitError(cmd, chat, err)
		}
		if err := e.transport.SendVoice(ctx, chat, cmd.Path); err != nil {
			return e.emitError(cmd, chat, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat)
	case "send_audio":
		if err := e.prepareActionBaseline(ctx, chat); err != nil {
			return e.emitError(cmd, chat, err)
		}
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
	e.lastState = &snapshot
	e.currentChat = chat
	messageID, callbackData, err := findButton(snapshot, cmd.ButtonText, cmd.MessageOffset)
	if err != nil {
		if cmd.MessageOffset != 0 {
			return err
		}
		messageID, callbackData, snapshot, err = e.waitForClickableButton(ctx, chat, cmd, snapshot)
		if err != nil {
			return err
		}
		e.lastState = &snapshot
		e.currentChat = chat
	}
	for attempt := 0; attempt < 2; attempt++ {
		data, err := base64.StdEncoding.DecodeString(callbackData)
		if err != nil {
			return fmt.Errorf("decode callback data: %w", err)
		}
		if err := e.transport.ClickButton(ctx, chat, messageID, data); err != nil {
			if !isClickDispatchAmbiguous(err) {
				return err
			}
			if err := e.waitForPostClickVisibleChange(ctx, chat, cmd, snapshot); err == nil {
				return nil
			}
			if attempt == 0 {
				retryMessageID, retryCallbackData, retrySnapshot, retryErr := e.resolveRetryableButton(ctx, chat, cmd, snapshot)
				if retryErr == nil {
					messageID = retryMessageID
					callbackData = retryCallbackData
					snapshot = retrySnapshot
					e.lastState = &snapshot
					e.currentChat = chat
					continue
				}
			}
			return fmt.Errorf("click_button dispatch was ambiguous and no visible effect was observed: %w", err)
		}
		return nil
	}
	return nil
}

func (e *Engine) prepareActionBaseline(ctx context.Context, chat string) error {
	if e.lastState != nil && e.currentChat == chat {
		return nil
	}
	snapshot, err := e.sync(ctx, chat)
	if err != nil {
		return err
	}
	e.lastState = &snapshot
	e.currentChat = chat
	return nil
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
		timeout = 30 * time.Second
	}

	if e.pendingWait != nil && e.pendingWait.chat == chat {
		pending := e.pendingWait
		e.pendingWait = nil
		e.lastState = &pending.snapshot
		e.currentChat = chat
		if pending.diff.HasChanges() {
			return e.emit(protocol.Event{
				Type:      "state_update",
				CommandID: cmd.ID,
				Action:    cmd.Action,
				Chat:      chat,
				OK:        true,
				Message:   "visible chat state already changed after previous action",
				Snapshot:  &pending.snapshot,
				Diff:      &pending.diff,
			})
		}
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
			snapshot, err := e.transport.SyncChat(ctx, chat, e.historyWindow)
			if err != nil {
				return e.emitError(cmd, chat, err)
			}
			diff := state.Diff(baseline, snapshot)
			if !diff.HasChanges() {
				continue
			}
			if !isWaitRelevantChange(baseline, snapshot, diff) {
				e.lastState = &snapshot
				e.currentChat = chat
				baseline = snapshot
				continue
			}
			e.lastState = &snapshot
			e.currentChat = chat
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
		return e.emit(protocol.Event{
			Type:      "state_snapshot",
			CommandID: cmd.ID,
			Action:    cmd.Action,
			Chat:      chat,
			OK:        true,
			Message:   "chat snapshot captured",
			Snapshot:  &snapshot,
		})
	}

	previous := *e.lastState
	diff := state.Diff(previous, snapshot)
	e.lastState = &snapshot
	if diff.HasChanges() {
		if shouldStorePendingWait(previous, snapshot, diff) {
			e.pendingWait = &pendingVisibleChange{
				chat:     chat,
				snapshot: snapshot,
				diff:     diff,
			}
		}
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

func (e *Engine) waitForClickableButton(ctx context.Context, chat string, cmd protocol.Command, baseline state.ChatState) (int, string, state.ChatState, error) {
	baselineInteractive, _ := latestInteractiveMessage(baseline)
	timeout := time.Duration(cmd.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(e.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return 0, "", state.ChatState{}, ctx.Err()
		case <-deadline.C:
			return 0, "", state.ChatState{}, fmt.Errorf("button %q did not appear within %s", cmd.ButtonText, timeout)
		case <-ticker.C:
			snapshot, err := e.transport.SyncChat(ctx, chat, e.historyWindow)
			if err != nil {
				return 0, "", state.ChatState{}, err
			}
			currentInteractive, ok := latestInteractiveMessage(snapshot)
			if !ok {
				continue
			}
			if baselineInteractive.ID == currentInteractive.ID && baselineInteractive.Text == currentInteractive.Text && buttonsEqual(baselineInteractive.Buttons, currentInteractive.Buttons) {
				continue
			}
			messageID, callbackData, err := findButton(snapshot, cmd.ButtonText, 0)
			if err != nil {
				baselineInteractive = currentInteractive
				continue
			}
			return messageID, callbackData, snapshot, nil
		}
	}
}

func (e *Engine) waitForPostClickVisibleChange(ctx context.Context, chat string, cmd protocol.Command, baseline state.ChatState) error {
	timeout := time.Duration(cmd.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(e.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("no visible change after %s", timeout)
		case <-ticker.C:
			snapshot, err := e.transport.SyncChat(ctx, chat, e.historyWindow)
			if err != nil {
				return err
			}
			diff := state.Diff(baseline, snapshot)
			if !diff.HasChanges() || !isWaitRelevantChange(baseline, snapshot, diff) {
				continue
			}
			return nil
		}
	}
}

func (e *Engine) resolveRetryableButton(ctx context.Context, chat string, cmd protocol.Command, baseline state.ChatState) (int, string, state.ChatState, error) {
	snapshot, err := e.transport.SyncChat(ctx, chat, e.historyWindow)
	if err != nil {
		return 0, "", state.ChatState{}, err
	}
	if diff := state.Diff(baseline, snapshot); isWaitRelevantChange(baseline, snapshot, diff) {
		return 0, "", state.ChatState{}, fmt.Errorf("visible state already changed")
	}

	messageID, callbackData, err := findButton(snapshot, cmd.ButtonText, cmd.MessageOffset)
	if err != nil {
		return 0, "", state.ChatState{}, err
	}
	return messageID, callbackData, snapshot, nil
}

func shouldStorePendingWait(prev, next state.ChatState, diff state.ChatDiff) bool {
	if diff.PinnedChanged {
		return true
	}

	nextByID := make(map[int]state.VisibleMessage, len(next.Messages))
	for _, msg := range next.Messages {
		nextByID[msg.ID] = msg
	}

	prevByID := make(map[int]state.VisibleMessage, len(prev.Messages))
	for _, msg := range prev.Messages {
		prevByID[msg.ID] = msg
	}

	for _, id := range diff.Added {
		msg, ok := nextByID[id]
		if !ok {
			continue
		}
		if !isPureSelfOutgoing(msg) {
			return true
		}
	}

	for _, id := range diff.Changed {
		nextMsg, nextOK := nextByID[id]
		prevMsg, prevOK := prevByID[id]
		if !nextOK || !prevOK {
			return true
		}
		if !isPureSelfOutgoing(nextMsg) || !isPureSelfOutgoing(prevMsg) {
			return true
		}
	}

	return false
}

func isWaitRelevantDiff(diff state.ChatDiff) bool {
	return diff.PinnedChanged || len(diff.Added) > 0 || len(diff.Changed) > 0
}

func isWaitRelevantChange(prev, next state.ChatState, diff state.ChatDiff) bool {
	if !isWaitRelevantDiff(diff) {
		return false
	}
	if diff.PinnedChanged {
		return true
	}

	nextByID := make(map[int]state.VisibleMessage, len(next.Messages))
	for _, msg := range next.Messages {
		nextByID[msg.ID] = msg
	}

	prevByID := make(map[int]state.VisibleMessage, len(prev.Messages))
	for _, msg := range prev.Messages {
		prevByID[msg.ID] = msg
	}

	for _, id := range diff.Added {
		msg, ok := nextByID[id]
		if !ok {
			continue
		}
		if !isPureSelfOutgoing(msg) {
			return true
		}
	}

	for _, id := range diff.Changed {
		nextMsg, nextOK := nextByID[id]
		prevMsg, prevOK := prevByID[id]
		if !nextOK || !prevOK {
			return true
		}
		if !isPureSelfOutgoing(nextMsg) || !isPureSelfOutgoing(prevMsg) {
			return true
		}
	}

	return false
}

func isPureSelfOutgoing(msg state.VisibleMessage) bool {
	return msg.Outgoing && msg.Sender == "self"
}

func (e *Engine) selectChat(ctx context.Context, cmd protocol.Command, chat string) error {
	e.pendingWait = nil
	if err := e.emit(protocol.Event{
		Type:      "ack",
		CommandID: cmd.ID,
		Action:    cmd.Action,
		Chat:      chat,
		OK:        true,
		Message:   "chat selected",
	}); err != nil {
		return err
	}

	snapshot, err := e.sync(ctx, chat)
	if err != nil {
		return e.emitError(cmd, chat, err)
	}
	e.lastState = &snapshot
	e.currentChat = chat
	return e.emit(protocol.Event{
		Type:      "state_snapshot",
		CommandID: cmd.ID,
		Action:    cmd.Action,
		Chat:      chat,
		OK:        true,
		Message:   "chat snapshot captured",
		Snapshot:  &snapshot,
	})
}

func (e *Engine) sync(ctx context.Context, chat string) (state.ChatState, error) {
	snapshot, err := e.transport.SyncChat(ctx, chat, e.historyWindow)
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
	return "", fmt.Errorf("chat is required for %s; use select_chat first or pass chat explicitly", cmd.Action)
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

func findButton(snapshot state.ChatState, buttonText string, messageOffset int) (int, string, error) {
	if messageOffset > 0 {
		return findButtonWithOffset(snapshot, buttonText, messageOffset)
	}
	if snapshot.Pinned != nil {
		callbackData, ok, err := findCallbackButtonInRows(snapshot.Pinned.Buttons, buttonText)
		if err != nil {
			return 0, "", err
		}
		if ok {
			return snapshot.Pinned.MessageID, callbackData, nil
		}
	}

	messages := interactiveMessages(snapshot)
	if len(messages) == 0 {
		return 0, "", fmt.Errorf("no visible interactive message found")
	}

	for _, msg := range messages {
		if isExactDuplicateOfPinned(snapshot.Pinned, msg) {
			continue
		}
		callbackData, ok, err := findCallbackButtonInRows(msg.Buttons, buttonText)
		if err != nil {
			return 0, "", err
		}
		if ok {
			return msg.ID, callbackData, nil
		}
	}

	if snapshot.Pinned != nil {
		return 0, "", fmt.Errorf("button %q not found in pinned or visible interactive messages", buttonText)
	}
	return 0, "", fmt.Errorf("button %q not found in visible interactive messages", buttonText)
}

func findButtonWithOffset(snapshot state.ChatState, buttonText string, messageOffset int) (int, string, error) {
	messages := interactiveMessages(snapshot)
	if len(messages) == 0 {
		return 0, "", fmt.Errorf("no visible interactive message found")
	}
	targetIndex := messageOffset - 1
	matchedMessageCount := 0
	for _, msg := range messages[1:] {
		callbackData, ok, err := findCallbackButtonInRows(msg.Buttons, buttonText)
		if err != nil {
			return 0, "", err
		}
		if !ok {
			continue
		}
		if matchedMessageCount != targetIndex {
			matchedMessageCount++
			continue
		}
		return msg.ID, callbackData, nil
	}
	return 0, "", fmt.Errorf("button %q not found with message_offset=%d", buttonText, messageOffset)
}

func findCallbackButtonInRows(rows [][]state.InlineButton, buttonText string) (string, bool, error) {
	for _, row := range rows {
		for _, button := range row {
			if button.Text != buttonText {
				continue
			}
			if button.Kind != "callback" || button.CallbackData == "" {
				return "", false, fmt.Errorf("button %q is not a callback button", buttonText)
			}
			return button.CallbackData, true, nil
		}
	}
	return "", false, nil
}

func isExactDuplicateOfPinned(pinned *state.PinnedMessage, msg state.VisibleMessage) bool {
	if pinned == nil {
		return false
	}
	if msg.ID == pinned.MessageID {
		return false
	}
	return msg.Text == pinned.Text && buttonsEqual(msg.Buttons, pinned.Buttons)
}

func interactiveMessages(snapshot state.ChatState) []state.VisibleMessage {
	botMessages := make([]state.VisibleMessage, 0, len(snapshot.Messages))
	for i := len(snapshot.Messages) - 1; i >= 0; i-- {
		msg := snapshot.Messages[i]
		if len(msg.Buttons) == 0 || msg.Sender != "bot" {
			continue
		}
		botMessages = append(botMessages, msg)
	}
	if len(botMessages) > 0 {
		return botMessages
	}
	messages := make([]state.VisibleMessage, 0, len(snapshot.Messages))
	for i := len(snapshot.Messages) - 1; i >= 0; i-- {
		msg := snapshot.Messages[i]
		if len(msg.Buttons) == 0 {
			continue
		}
		messages = append(messages, msg)
	}
	return messages
}

func latestInteractiveMessage(snapshot state.ChatState) (state.VisibleMessage, bool) {
	messages := interactiveMessages(snapshot)
	if len(messages) == 0 {
		return state.VisibleMessage{}, false
	}
	return messages[0], true
}

func buttonsEqual(a, b [][]state.InlineButton) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

func isClickDispatchAmbiguous(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "i/o timeout") {
		return true
	}
	return strings.Contains(strings.ToUpper(err.Error()), "MESSAGE_ID_INVALID")
}
