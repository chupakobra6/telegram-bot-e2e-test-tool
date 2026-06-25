package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/state"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/transcript"
)

type Transport interface {
	SendText(ctx context.Context, chat string, messageThreadID int, text string) error
	EditText(ctx context.Context, chat string, messageThreadID int, messageID int, text string) error
	SendPhoto(ctx context.Context, chat string, messageThreadID int, path string, caption string) error
	SendPhotoGroup(ctx context.Context, chat string, messageThreadID int, paths []string, caption string) error
	SendDocument(ctx context.Context, chat string, messageThreadID int, path string, caption string) error
	SendVoice(ctx context.Context, chat string, messageThreadID int, path string) error
	SendAudio(ctx context.Context, chat string, messageThreadID int, path string) error
	ClickButton(ctx context.Context, chat string, messageThreadID int, messageID int, data []byte) error
	DeleteMessages(ctx context.Context, chat string, messageThreadID int, messageIDs []int) error
	SyncChat(ctx context.Context, chat string, messageThreadID int, limit int) (state.ChatState, error)
}

const defaultDeleteVisibleMessageBatches = 8

type Engine struct {
	transport     Transport
	transcript    *transcript.Transcript
	out           io.Writer
	historyWindow int
	syncInterval  time.Duration

	currentChat     string
	currentThreadID int
	lastState       *state.ChatState
	lastStateChat   string
	lastStateThread int
	pendingWait     *pendingVisibleChange
}

type pendingVisibleChange struct {
	chat            string
	messageThreadID int
	snapshot        state.ChatState
	diff            state.ChatDiff
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
	chat, messageThreadID, err := e.resolveTarget(cmd)
	if err != nil {
		return e.emitError(cmd, chat, messageThreadID, err)
	}

	switch cmd.Action {
	case "select_chat":
		return e.selectChat(ctx, cmd, chat, messageThreadID)
	case "send_text":
		if err := e.prepareActionBaseline(ctx, chat, messageThreadID); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		if err := e.transport.SendText(ctx, chat, messageThreadID, cmd.Text); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat, messageThreadID)
	case "send_bot_note":
		if err := e.prepareActionBaseline(ctx, chat, messageThreadID); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		if err := sendBotNote(ctx, cmd.Text); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat, messageThreadID)
	case "edit_text":
		if err := e.executeEditText(ctx, cmd, chat, messageThreadID); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat, messageThreadID)
	case "send_photo":
		if err := e.prepareActionBaseline(ctx, chat, messageThreadID); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		if err := e.transport.SendPhoto(ctx, chat, messageThreadID, cmd.Path, cmd.Caption); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat, messageThreadID)
	case "send_photo_group":
		if err := e.prepareActionBaseline(ctx, chat, messageThreadID); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		if err := e.transport.SendPhotoGroup(ctx, chat, messageThreadID, cmd.Paths, cmd.Caption); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat, messageThreadID)
	case "send_document":
		if err := e.prepareActionBaseline(ctx, chat, messageThreadID); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		if err := e.transport.SendDocument(ctx, chat, messageThreadID, cmd.Path, cmd.Caption); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat, messageThreadID)
	case "send_voice":
		if err := e.prepareActionBaseline(ctx, chat, messageThreadID); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		if err := e.transport.SendVoice(ctx, chat, messageThreadID, cmd.Path); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat, messageThreadID)
	case "send_audio":
		if err := e.prepareActionBaseline(ctx, chat, messageThreadID); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		if err := e.transport.SendAudio(ctx, chat, messageThreadID, cmd.Path); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat, messageThreadID)
	case "click_button":
		if err := e.executeClick(ctx, cmd, chat, messageThreadID); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		return e.emitAckAndSync(ctx, cmd, chat, messageThreadID)
	case "delete_visible_messages":
		if err := e.executeDeleteVisibleMessages(ctx, cmd, chat, messageThreadID); err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		return nil
	case "assert_visible_text", "assert_not_visible_text":
		return e.executeTextAssertion(ctx, cmd, chat, messageThreadID)
	case "dump_state":
		snapshot, err := e.sync(ctx, chat, messageThreadID)
		if err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		e.setLastState(chat, messageThreadID, snapshot)
		return e.emit(protocol.Event{
			Type:            "state_snapshot",
			CommandID:       cmd.ID,
			Action:          cmd.Action,
			Chat:            chat,
			MessageThreadID: messageThreadID,
			OK:              true,
			Message:         "chat snapshot captured",
			Snapshot:        &snapshot,
		})
	case "wait":
		return e.executeWait(ctx, cmd, chat, messageThreadID)
	case "sleep":
		return e.executeSleep(ctx, cmd, chat, messageThreadID)
	default:
		return e.emitError(cmd, chat, messageThreadID, fmt.Errorf("unsupported action %q", cmd.Action))
	}
}

func (e *Engine) executeSleep(ctx context.Context, cmd protocol.Command, chat string, messageThreadID int) error {
	duration := time.Duration(cmd.TimeoutMS) * time.Millisecond
	if err := e.emit(protocol.Event{
		Type:            "ack",
		CommandID:       cmd.ID,
		Action:          cmd.Action,
		Chat:            chat,
		MessageThreadID: messageThreadID,
		OK:              true,
		Message:         fmt.Sprintf("sleeping for %s", duration),
	}); err != nil {
		return err
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return e.emitError(cmd, chat, messageThreadID, ctx.Err())
	case <-timer.C:
		return e.emit(protocol.Event{
			Type:            "ack",
			CommandID:       cmd.ID,
			Action:          cmd.Action,
			Chat:            chat,
			MessageThreadID: messageThreadID,
			OK:              true,
			Message:         "sleep completed",
		})
	}
}

func sendBotNote(ctx context.Context, text string) error {
	token := strings.TrimSpace(os.Getenv("TG_E2E_BOT_TOKEN"))
	if token == "" {
		return fmt.Errorf("TG_E2E_BOT_TOKEN is required for send_bot_note")
	}
	chatID := strings.TrimSpace(os.Getenv("TG_E2E_BOT_CHAT_ID"))
	if chatID == "" {
		return fmt.Errorf("TG_E2E_BOT_CHAT_ID is required for send_bot_note")
	}
	payload, err := json.Marshal(map[string]any{
		"chat_id": chatID,
		"text":    text,
	})
	if err != nil {
		return fmt.Errorf("encode bot note: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+token+"/sendMessage", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build bot note request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send bot note request failed")
	}
	defer resp.Body.Close()
	var decoded struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return fmt.Errorf("decode bot note response: %w", err)
	}
	if !decoded.OK {
		description := strings.TrimSpace(decoded.Description)
		if description == "" {
			description = resp.Status
		}
		return fmt.Errorf("send bot note failed: %s", description)
	}
	return nil
}

func (e *Engine) executeTextAssertion(ctx context.Context, cmd protocol.Command, chat string, messageThreadID int) error {
	timeout := time.Duration(cmd.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(e.syncInterval)
	defer ticker.Stop()

	var lastSnapshot state.ChatState
	var lastErr error
	for {
		snapshot, err := e.sync(ctx, chat, messageThreadID)
		if err != nil {
			lastErr = err
		} else {
			lastSnapshot = snapshot
			e.setLastState(chat, messageThreadID, snapshot)
			e.setCurrentTarget(chat, messageThreadID)
			found := visibleTextExists(snapshot, cmd.Text)
			if (cmd.Action == "assert_visible_text" && found) || (cmd.Action == "assert_not_visible_text" && !found) {
				return e.emit(protocol.Event{
					Type:            "state_snapshot",
					CommandID:       cmd.ID,
					Action:          cmd.Action,
					Chat:            chat,
					MessageThreadID: messageThreadID,
					OK:              true,
					Message:         "visible text assertion passed",
					Snapshot:        &snapshot,
				})
			}
			if cmd.Action == "assert_visible_text" {
				lastErr = fmt.Errorf("visible text %q not found", cmd.Text)
			} else {
				lastErr = fmt.Errorf("visible text %q is still present", cmd.Text)
			}
		}

		select {
		case <-ctx.Done():
			return e.emitError(cmd, chat, messageThreadID, ctx.Err())
		case <-deadline.C:
			event := protocol.Event{
				Type:            "timeout",
				CommandID:       cmd.ID,
				Action:          cmd.Action,
				Chat:            chat,
				MessageThreadID: messageThreadID,
				Error:           lastErr.Error(),
				Snapshot:        &lastSnapshot,
			}
			if err := e.emit(event); err != nil {
				return err
			}
			return lastErr
		case <-ticker.C:
		}
	}
}

func (e *Engine) executeEditText(ctx context.Context, cmd protocol.Command, chat string, messageThreadID int) error {
	snapshot, err := e.sync(ctx, chat, messageThreadID)
	if err != nil {
		return err
	}
	e.setLastState(chat, messageThreadID, snapshot)
	e.setCurrentTarget(chat, messageThreadID)
	messageID, err := findEditableTextMessage(snapshot, cmd.MessageOffset, cmd.MessageText)
	if err != nil {
		return err
	}
	return e.transport.EditText(ctx, chat, messageThreadID, messageID, cmd.Text)
}

func (e *Engine) executeClick(ctx context.Context, cmd protocol.Command, chat string, messageThreadID int) error {
	snapshot, err := e.sync(ctx, chat, messageThreadID)
	if err != nil {
		return err
	}
	e.setLastState(chat, messageThreadID, snapshot)
	e.setCurrentTarget(chat, messageThreadID)
	messageID, callbackData, err := findButton(snapshot, cmd.ButtonText, cmd.MessageOffset, cmd.MessageText)
	if err != nil {
		if cmd.MessageOffset != 0 {
			return err
		}
		messageID, callbackData, snapshot, err = e.waitForClickableButton(ctx, chat, messageThreadID, cmd, snapshot)
		if err != nil {
			return err
		}
		e.setLastState(chat, messageThreadID, snapshot)
		e.setCurrentTarget(chat, messageThreadID)
	}
	for attempt := 0; attempt < 2; attempt++ {
		data, err := base64.StdEncoding.DecodeString(callbackData)
		if err != nil {
			return fmt.Errorf("decode callback data: %w", err)
		}
		if err := e.transport.ClickButton(ctx, chat, messageThreadID, messageID, data); err != nil {
			if !isClickDispatchAmbiguous(err) {
				return err
			}
			if err := e.waitForPostClickVisibleChange(ctx, chat, messageThreadID, cmd, snapshot); err == nil {
				return nil
			}
			if attempt == 0 {
				retryMessageID, retryCallbackData, retrySnapshot, retryErr := e.resolveRetryableButton(ctx, chat, messageThreadID, cmd, snapshot)
				if retryErr == nil {
					messageID = retryMessageID
					callbackData = retryCallbackData
					snapshot = retrySnapshot
					e.setLastState(chat, messageThreadID, snapshot)
					e.setCurrentTarget(chat, messageThreadID)
					continue
				}
			}
			return fmt.Errorf("click_button dispatch was ambiguous and no visible effect was observed: %w", err)
		}
		return nil
	}
	return nil
}

func (e *Engine) executeDeleteVisibleMessages(ctx context.Context, cmd protocol.Command, chat string, messageThreadID int) error {
	if err := e.emit(protocol.Event{
		Type:            "ack",
		CommandID:       cmd.ID,
		Action:          cmd.Action,
		Chat:            chat,
		MessageThreadID: messageThreadID,
		OK:              true,
		Message:         "deleting visible chat messages",
	}); err != nil {
		return err
	}

	maxBatches := cmd.MaxBatches
	if maxBatches <= 0 {
		maxBatches = defaultDeleteVisibleMessageBatches
	}

	deleted := 0
	limited := false
	for batch := 0; batch < maxBatches; batch++ {
		snapshot, err := e.sync(ctx, chat, messageThreadID)
		if err != nil {
			return err
		}
		messageIDs := deletableVisibleMessageIDs(snapshot, messageThreadID, cmd.MessageText, cmd.KeepOldest)
		if len(messageIDs) == 0 {
			e.setLastState(chat, messageThreadID, snapshot)
			return e.emitDeleteSummary(ctx, cmd, chat, messageThreadID, deleted, false)
		}
		if err := e.transport.DeleteMessages(ctx, chat, messageThreadID, messageIDs); err != nil {
			return err
		}
		deleted += len(messageIDs)
	}
	limited = true
	return e.emitDeleteSummary(ctx, cmd, chat, messageThreadID, deleted, limited)
}

func (e *Engine) emitDeleteSummary(ctx context.Context, cmd protocol.Command, chat string, messageThreadID int, deleted int, limited bool) error {
	snapshot, err := e.sync(ctx, chat, messageThreadID)
	if err != nil {
		return err
	}
	e.setLastState(chat, messageThreadID, snapshot)
	message := fmt.Sprintf("deleted %d visible messages", deleted)
	if limited {
		message = fmt.Sprintf("deleted %d visible messages; max_batches reached", deleted)
	}
	return e.emit(protocol.Event{
		Type:            "state_snapshot",
		CommandID:       cmd.ID,
		Action:          cmd.Action,
		Chat:            chat,
		MessageThreadID: messageThreadID,
		OK:              true,
		Message:         message,
		Snapshot:        &snapshot,
	})
}

func deletableVisibleMessageIDs(snapshot state.ChatState, messageThreadID int, messageText string, keepOldest bool) []int {
	preserveOldest := keepOldest && strings.TrimSpace(messageText) == ""
	oldestID := 0
	if preserveOldest {
		for _, msg := range snapshot.Messages {
			if msg.ID <= 0 || msg.ID == messageThreadID || msg.Kind == "service" || msg.Pinned {
				continue
			}
			if oldestID == 0 || msg.ID < oldestID {
				oldestID = msg.ID
			}
		}
	}

	pinnedID := 0
	if snapshot.Pinned != nil {
		pinnedID = snapshot.Pinned.MessageID
	}

	seen := map[int]struct{}{}
	messageIDs := make([]int, 0, len(snapshot.Messages))
	for _, msg := range snapshot.Messages {
		if msg.ID <= 0 {
			continue
		}
		if msg.ID == messageThreadID || msg.ID == pinnedID || msg.ID == oldestID || msg.Pinned || msg.Kind == "service" {
			continue
		}
		if !messageMatches(msg.Text, messageText) {
			continue
		}
		if _, ok := seen[msg.ID]; ok {
			continue
		}
		seen[msg.ID] = struct{}{}
		messageIDs = append(messageIDs, msg.ID)
	}
	sort.Ints(messageIDs)
	return messageIDs
}

func (e *Engine) prepareActionBaseline(ctx context.Context, chat string, messageThreadID int) error {
	if e.hasLastStateFor(chat, messageThreadID) {
		return nil
	}
	snapshot, err := e.sync(ctx, chat, messageThreadID)
	if err != nil {
		return err
	}
	e.setLastState(chat, messageThreadID, snapshot)
	e.setCurrentTarget(chat, messageThreadID)
	return nil
}

func (e *Engine) executeWait(ctx context.Context, cmd protocol.Command, chat string, messageThreadID int) error {
	if err := e.emit(protocol.Event{
		Type:            "ack",
		CommandID:       cmd.ID,
		Action:          cmd.Action,
		Chat:            chat,
		MessageThreadID: messageThreadID,
		OK:              true,
		Message:         "waiting for visible chat changes",
	}); err != nil {
		return err
	}

	timeout := time.Duration(cmd.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	if e.pendingWait != nil && e.pendingWait.chat == chat && e.pendingWait.messageThreadID == messageThreadID {
		pending := e.pendingWait
		e.pendingWait = nil
		e.setLastState(chat, messageThreadID, pending.snapshot)
		e.setCurrentTarget(chat, messageThreadID)
		if pending.diff.HasChanges() {
			return e.emit(protocol.Event{
				Type:            "state_update",
				CommandID:       cmd.ID,
				Action:          cmd.Action,
				Chat:            chat,
				MessageThreadID: messageThreadID,
				OK:              true,
				Message:         "visible chat state already changed after previous action",
				Snapshot:        &pending.snapshot,
				Diff:            &pending.diff,
			})
		}
	}

	var baseline state.ChatState
	if e.hasLastStateFor(chat, messageThreadID) {
		baseline = *e.lastState
	} else {
		snapshot, err := e.sync(ctx, chat, messageThreadID)
		if err != nil {
			return e.emitError(cmd, chat, messageThreadID, err)
		}
		e.setLastState(chat, messageThreadID, snapshot)
		baseline = snapshot
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(e.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return e.emitError(cmd, chat, messageThreadID, ctx.Err())
		case <-deadline.C:
			timeoutErr := fmt.Errorf("wait timeout after %s", timeout)
			if err := e.emit(protocol.Event{
				Type:            "timeout",
				CommandID:       cmd.ID,
				Action:          cmd.Action,
				Chat:            chat,
				MessageThreadID: messageThreadID,
				Error:           timeoutErr.Error(),
				Snapshot:        &baseline,
			}); err != nil {
				return err
			}
			return timeoutErr
		case <-ticker.C:
			snapshot, err := e.transport.SyncChat(ctx, chat, messageThreadID, e.historyWindow)
			if err != nil {
				return e.emitError(cmd, chat, messageThreadID, err)
			}
			diff := state.Diff(baseline, snapshot)
			if !diff.HasChanges() {
				continue
			}
			if !isWaitRelevantChange(baseline, snapshot, diff) {
				e.setLastState(chat, messageThreadID, snapshot)
				e.setCurrentTarget(chat, messageThreadID)
				baseline = snapshot
				continue
			}
			e.setLastState(chat, messageThreadID, snapshot)
			e.setCurrentTarget(chat, messageThreadID)
			return e.emit(protocol.Event{
				Type:            "state_update",
				CommandID:       cmd.ID,
				Action:          cmd.Action,
				Chat:            chat,
				MessageThreadID: messageThreadID,
				OK:              true,
				Message:         "visible chat state changed",
				Snapshot:        &snapshot,
				Diff:            &diff,
			})
		}
	}
}

func (e *Engine) emitAckAndSync(ctx context.Context, cmd protocol.Command, chat string, messageThreadID int) error {
	if err := e.emit(protocol.Event{
		Type:            "ack",
		CommandID:       cmd.ID,
		Action:          cmd.Action,
		Chat:            chat,
		MessageThreadID: messageThreadID,
		OK:              true,
		Message:         "command executed",
	}); err != nil {
		return err
	}

	snapshot, err := e.sync(ctx, chat, messageThreadID)
	if err != nil {
		return e.emitError(cmd, chat, messageThreadID, err)
	}
	if !e.hasLastStateFor(chat, messageThreadID) {
		e.setLastState(chat, messageThreadID, snapshot)
		return e.emit(protocol.Event{
			Type:            "state_snapshot",
			CommandID:       cmd.ID,
			Action:          cmd.Action,
			Chat:            chat,
			MessageThreadID: messageThreadID,
			OK:              true,
			Message:         "chat snapshot captured",
			Snapshot:        &snapshot,
		})
	}

	previous := *e.lastState
	diff := state.Diff(previous, snapshot)
	e.setLastState(chat, messageThreadID, snapshot)
	if diff.HasChanges() {
		if shouldStorePendingWait(previous, snapshot, diff) {
			e.pendingWait = &pendingVisibleChange{
				chat:            chat,
				messageThreadID: messageThreadID,
				snapshot:        snapshot,
				diff:            diff,
			}
		}
		return e.emit(protocol.Event{
			Type:            "state_update",
			CommandID:       cmd.ID,
			Action:          cmd.Action,
			Chat:            chat,
			MessageThreadID: messageThreadID,
			OK:              true,
			Message:         "visible chat state changed",
			Snapshot:        &snapshot,
			Diff:            &diff,
		})
	}
	return e.emit(protocol.Event{
		Type:            "state_snapshot",
		CommandID:       cmd.ID,
		Action:          cmd.Action,
		Chat:            chat,
		MessageThreadID: messageThreadID,
		OK:              true,
		Message:         "visible chat state unchanged",
		Snapshot:        &snapshot,
	})
}

func (e *Engine) waitForClickableButton(ctx context.Context, chat string, messageThreadID int, cmd protocol.Command, baseline state.ChatState) (int, string, state.ChatState, error) {
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
			snapshot, err := e.transport.SyncChat(ctx, chat, messageThreadID, e.historyWindow)
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
			messageID, callbackData, err := findButton(snapshot, cmd.ButtonText, 0, cmd.MessageText)
			if err != nil {
				baselineInteractive = currentInteractive
				continue
			}
			return messageID, callbackData, snapshot, nil
		}
	}
}

func (e *Engine) waitForPostClickVisibleChange(ctx context.Context, chat string, messageThreadID int, cmd protocol.Command, baseline state.ChatState) error {
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
			snapshot, err := e.transport.SyncChat(ctx, chat, messageThreadID, e.historyWindow)
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

func (e *Engine) resolveRetryableButton(ctx context.Context, chat string, messageThreadID int, cmd protocol.Command, baseline state.ChatState) (int, string, state.ChatState, error) {
	snapshot, err := e.transport.SyncChat(ctx, chat, messageThreadID, e.historyWindow)
	if err != nil {
		return 0, "", state.ChatState{}, err
	}
	if diff := state.Diff(baseline, snapshot); isWaitRelevantChange(baseline, snapshot, diff) {
		return 0, "", state.ChatState{}, fmt.Errorf("visible state already changed")
	}

	messageID, callbackData, err := findButton(snapshot, cmd.ButtonText, cmd.MessageOffset, cmd.MessageText)
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

func (e *Engine) selectChat(ctx context.Context, cmd protocol.Command, chat string, messageThreadID int) error {
	e.pendingWait = nil
	if err := e.emit(protocol.Event{
		Type:            "ack",
		CommandID:       cmd.ID,
		Action:          cmd.Action,
		Chat:            chat,
		MessageThreadID: messageThreadID,
		OK:              true,
		Message:         "chat selected",
	}); err != nil {
		return err
	}

	snapshot, err := e.sync(ctx, chat, messageThreadID)
	if err != nil {
		return e.emitError(cmd, chat, messageThreadID, err)
	}
	e.setLastState(chat, messageThreadID, snapshot)
	e.setCurrentTarget(chat, messageThreadID)
	return e.emit(protocol.Event{
		Type:            "state_snapshot",
		CommandID:       cmd.ID,
		Action:          cmd.Action,
		Chat:            chat,
		MessageThreadID: messageThreadID,
		OK:              true,
		Message:         "chat snapshot captured",
		Snapshot:        &snapshot,
	})
}

func (e *Engine) sync(ctx context.Context, chat string, messageThreadID int) (state.ChatState, error) {
	snapshot, err := e.transport.SyncChat(ctx, chat, messageThreadID, e.historyWindow)
	if err != nil {
		return state.ChatState{}, err
	}
	e.setCurrentTarget(chat, messageThreadID)
	return snapshot, nil
}

func (e *Engine) resolveTarget(cmd protocol.Command) (string, int, error) {
	if strings.TrimSpace(cmd.Chat) != "" {
		chat := strings.TrimSpace(cmd.Chat)
		messageThreadID := 0
		if cmd.HasThreadID() {
			messageThreadID = cmd.ThreadID()
		}
		e.setCurrentTarget(chat, messageThreadID)
		return chat, messageThreadID, nil
	}
	if e.currentChat != "" {
		if cmd.HasThreadID() {
			e.currentThreadID = cmd.ThreadID()
		}
		return e.currentChat, e.currentThreadID, nil
	}
	return "", 0, fmt.Errorf("chat is required for %s; use select_chat first or pass chat explicitly", cmd.Action)
}

func (e *Engine) setCurrentTarget(chat string, messageThreadID int) {
	e.currentChat = chat
	e.currentThreadID = messageThreadID
}

func (e *Engine) setLastState(chat string, messageThreadID int, snapshot state.ChatState) {
	e.lastState = &snapshot
	e.lastStateChat = chat
	e.lastStateThread = messageThreadID
}

func (e *Engine) hasLastStateFor(chat string, messageThreadID int) bool {
	return e.lastState != nil && e.lastStateChat == chat && e.lastStateThread == messageThreadID
}

func (e *Engine) emitError(cmd protocol.Command, chat string, messageThreadID int, err error) error {
	emitErr := e.emit(protocol.Event{
		Type:            "error",
		CommandID:       cmd.ID,
		Action:          cmd.Action,
		Chat:            chat,
		MessageThreadID: messageThreadID,
		Error:           err.Error(),
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

func findButton(snapshot state.ChatState, buttonText string, messageOffset int, messageText string) (int, string, error) {
	if messageOffset > 0 {
		return findButtonWithOffset(snapshot, buttonText, messageOffset, messageText)
	}
	if snapshot.Pinned != nil {
		if messageMatches(snapshot.Pinned.Text, messageText) {
			callbackData, ok, err := findCallbackButtonInRows(snapshot.Pinned.Buttons, buttonText)
			if err != nil {
				return 0, "", err
			}
			if ok {
				return snapshot.Pinned.MessageID, callbackData, nil
			}
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
		if !messageMatches(msg.Text, messageText) {
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

	if strings.TrimSpace(messageText) != "" {
		return 0, "", fmt.Errorf("button %q not found in messages containing %q", buttonText, messageText)
	}
	if snapshot.Pinned != nil {
		return 0, "", fmt.Errorf("button %q not found in pinned or visible interactive messages", buttonText)
	}
	return 0, "", fmt.Errorf("button %q not found in visible interactive messages", buttonText)
}

func findEditableTextMessage(snapshot state.ChatState, messageOffset int, messageText string) (int, error) {
	matches := make([]state.VisibleMessage, 0, len(snapshot.Messages))
	for i := len(snapshot.Messages) - 1; i >= 0; i-- {
		msg := snapshot.Messages[i]
		if msg.ID <= 0 || !msg.Outgoing || msg.Sender != "self" || msg.Kind == "service" {
			continue
		}
		if !messageMatches(msg.Text, messageText) {
			continue
		}
		matches = append(matches, msg)
	}
	if len(matches) == 0 {
		if strings.TrimSpace(messageText) != "" {
			return 0, fmt.Errorf("editable outgoing message containing %q not found", messageText)
		}
		return 0, fmt.Errorf("editable outgoing message not found")
	}
	if messageOffset >= len(matches) {
		return 0, fmt.Errorf("editable outgoing message offset %d out of range; matches=%d", messageOffset, len(matches))
	}
	return matches[messageOffset].ID, nil
}

func visibleTextExists(snapshot state.ChatState, text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	if snapshot.Pinned != nil && strings.Contains(snapshot.Pinned.Text, text) {
		return true
	}
	for _, msg := range snapshot.Messages {
		if strings.Contains(msg.Text, text) {
			return true
		}
	}
	return false
}

func findButtonWithOffset(snapshot state.ChatState, buttonText string, messageOffset int, messageText string) (int, string, error) {
	messages := interactiveMessages(snapshot)
	if len(messages) == 0 {
		return 0, "", fmt.Errorf("no visible interactive message found")
	}
	targetIndex := messageOffset - 1
	matchedMessageCount := 0
	for _, msg := range messages[1:] {
		if !messageMatches(msg.Text, messageText) {
			continue
		}
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

func messageMatches(text string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return true
	}
	return strings.Contains(text, want)
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
