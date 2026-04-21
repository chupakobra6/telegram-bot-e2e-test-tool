package triage

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/state"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/transcript"
)

const (
	pinnedExcerptLimit    = 120
	messageExcerptLimit   = 80
	failureWindowSize     = 14
	maxFinalButtons       = 6
	maxFailureStates      = 2
	maxFailureWindowAfter = failureWindowSize / 2
)

type CompactEvent struct {
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"`
	Type      string    `json:"type,omitempty"`
	CommandID string    `json:"command_id,omitempty"`
	Action    string    `json:"action,omitempty"`
	Target    string    `json:"target,omitempty"`
	State     string    `json:"state,omitempty"`
	Diff      string    `json:"diff,omitempty"`
	Message   string    `json:"message,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type CompactTranscript struct {
	StartedAt       time.Time      `json:"started_at"`
	Events          []CompactEvent `json:"events"`
	FinalStateLabel string         `json:"final_state_label,omitempty"`
	FinalButtons    []string       `json:"final_buttons,omitempty"`
}

type SummaryRow struct {
	ScenarioPath     string    `json:"scenario_path"`
	TranscriptLabel  string    `json:"transcript_label"`
	Status           string    `json:"status"`
	StartedAt        time.Time `json:"started_at"`
	FinishedAt       time.Time `json:"finished_at"`
	DurationMS       int64     `json:"duration_ms"`
	FailingCommandID string    `json:"failing_command_id,omitempty"`
	FailingAction    string    `json:"failing_action,omitempty"`
	TerminalError    string    `json:"terminal_error,omitempty"`
	FailureAt        time.Time `json:"failure_at,omitempty"`
	LastDiffSummary  string    `json:"last_diff_summary,omitempty"`
	FinalStateLabel  string    `json:"final_state_label,omitempty"`
	FinalButtons     []string  `json:"final_buttons,omitempty"`
	RawJSON          string    `json:"raw_json"`
	RawText          string    `json:"raw_text"`
	CompactJSON      string    `json:"compact_json"`
	CompactText      string    `json:"compact_text"`
}

type FailureTimelineEntry struct {
	CommandID  string    `json:"command_id,omitempty"`
	Action     string    `json:"action,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	DurationMS int64     `json:"duration_ms"`
}

type FailureReport struct {
	ScenarioPath           string                 `json:"scenario_path"`
	TranscriptLabel        string                 `json:"transcript_label"`
	Status                 string                 `json:"status"`
	StartedAt              time.Time              `json:"started_at"`
	FinishedAt             time.Time              `json:"finished_at"`
	DurationMS             int64                  `json:"duration_ms"`
	FailingCommandID       string                 `json:"failing_command_id,omitempty"`
	FailingAction          string                 `json:"failing_action,omitempty"`
	TerminalError          string                 `json:"terminal_error,omitempty"`
	FailureAt              time.Time              `json:"failure_at,omitempty"`
	LastDiffSummary        string                 `json:"last_diff_summary,omitempty"`
	FinalStateLabel        string                 `json:"final_state_label,omitempty"`
	FinalPinnedStateLabel  string                 `json:"final_pinned_state_label,omitempty"`
	FinalInteractiveStates []string               `json:"final_interactive_states,omitempty"`
	FinalButtons           []string               `json:"final_buttons,omitempty"`
	RawJSON                string                 `json:"raw_json"`
	RawText                string                 `json:"raw_text"`
	CompactJSON            string                 `json:"compact_json"`
	CompactText            string                 `json:"compact_text"`
	Timeline               []FailureTimelineEntry `json:"timeline,omitempty"`
	Window                 []CompactEvent         `json:"window,omitempty"`
}

type finalStateSummary struct {
	PrimaryStateLabel      string
	PinnedStateLabel       string
	InteractiveStateLabels []string
	Buttons                []string
}

var (
	reHomeTracked    = regexp.MustCompile(`Сейчас отслеживаем:\s*(\d+)`)
	reHomeSoon       = regexp.MustCompile(`Скоро истекают:\s*(\d+)`)
	reHomeExpired    = regexp.MustCompile(`Уже истекли:\s*(\d+)`)
	reStatsEaten     = regexp.MustCompile(`Съедены:\s*(\d+)`)
	reStatsWasted    = regexp.MustCompile(`Выброшены:\s*(\d+)`)
	reStatsDeleted   = regexp.MustCompile(`Удалены:\s*(\d+)`)
	reSettingsTZ     = regexp.MustCompile(`Часовой пояс:\s*([^\n|]+)`)
	reSettingsDigest = regexp.MustCompile(`Утренний дайджест:\s*([0-9]{2}:[0-9]{2})`)
	reDraftName      = regexp.MustCompile(`Название:\s*([^\n|]+)`)
	reDraftDate      = regexp.MustCompile(`Срок:\s*([^\n|]+)`)
	reDraftSource    = regexp.MustCompile(`Источник:\s*([^\n|]+)`)
	reWhitespace     = regexp.MustCompile(`\s+`)
)

func BuildCompactTranscript(tr *transcript.Transcript) CompactTranscript {
	compact := CompactTranscript{StartedAt: tr.StartedAt}
	var lastPinnedLabel string
	for _, event := range tr.Events {
		entry, pinnedLabel := compactEvent(event, lastPinnedLabel)
		if entry != nil {
			compact.Events = append(compact.Events, *entry)
		}
		if pinnedLabel != "" {
			lastPinnedLabel = pinnedLabel
		}
	}
	final := summarizeFinalState(lastSnapshot(tr))
	compact.FinalStateLabel = final.PrimaryStateLabel
	compact.FinalButtons = final.Buttons
	return compact
}

func (c CompactTranscript) RenderText() string {
	lines := []string{"Compact Transcript"}
	for _, event := range c.Events {
		var parts []string
		switch event.Kind {
		case "command":
			parts = append(parts, fmt.Sprintf("%s [command]", event.At.Format(time.RFC3339)))
			if event.CommandID != "" {
				parts = append(parts, "id="+event.CommandID)
			}
			if event.Action != "" {
				parts = append(parts, "action="+event.Action)
			}
			if event.Target != "" {
				parts = append(parts, "target="+event.Target)
			}
		default:
			parts = append(parts, fmt.Sprintf("%s [event]", event.At.Format(time.RFC3339)))
			if event.Type != "" {
				parts = append(parts, "type="+event.Type)
			}
			if event.CommandID != "" {
				parts = append(parts, "id="+event.CommandID)
			}
			if event.Action != "" {
				parts = append(parts, "action="+event.Action)
			}
			if event.State != "" {
				parts = append(parts, "state="+event.State)
			}
			if event.Diff != "" {
				parts = append(parts, "diff="+event.Diff)
			}
			if event.Message != "" {
				parts = append(parts, "msg="+event.Message)
			}
			if event.Error != "" {
				parts = append(parts, "error="+event.Error)
			}
		}
		lines = append(lines, strings.Join(parts, " "))
	}
	if c.FinalStateLabel != "" {
		lines = append(lines, "final_state="+c.FinalStateLabel)
	}
	if len(c.FinalButtons) > 0 {
		lines = append(lines, "final_buttons="+strings.Join(c.FinalButtons, ","))
	}
	return strings.Join(lines, "\n")
}

func BuildSummaryRow(tr *transcript.Transcript, scenarioPath, label, rawJSON, rawText, compactJSON, compactText string) SummaryRow {
	row := SummaryRow{
		ScenarioPath:    scenarioPath,
		TranscriptLabel: label,
		Status:          "passed",
		StartedAt:       tr.StartedAt,
		RawJSON:         rawJSON,
		RawText:         rawText,
		CompactJSON:     compactJSON,
		CompactText:     compactText,
	}
	if finished := transcriptFinishedAt(tr); !finished.IsZero() {
		row.FinishedAt = finished
		row.DurationMS = finished.Sub(tr.StartedAt).Milliseconds()
	} else {
		row.FinishedAt = tr.StartedAt
	}
	if failure := terminalFailure(tr); failure != nil {
		row.Status = "failed"
		row.FailingCommandID = failure.CommandID
		row.FailingAction = failure.Action
		row.TerminalError = failure.Error
		row.FailureAt = failure.At
	}
	if diff := lastDiffSummary(tr); diff != "" {
		row.LastDiffSummary = diff
	}
	final := summarizeFinalState(lastSnapshot(tr))
	row.FinalStateLabel = final.PrimaryStateLabel
	row.FinalButtons = final.Buttons
	return row
}

func RenderSummaryText(rows []SummaryRow) string {
	lines := []string{"Last Run Summary"}
	for _, row := range rows {
		status := row.Status
		if status == "" {
			status = "passed"
		}
		duration := fmt.Sprintf("%dms", row.DurationMS)
		failing := row.FailingAction
		if row.FailingCommandID != "" {
			failing = row.FailingCommandID + ":" + row.FailingAction
		}
		if failing == "" {
			failing = "-"
		}
		errText := row.TerminalError
		if errText == "" {
			errText = "-"
		}
		finalState := row.FinalStateLabel
		if finalState == "" {
			finalState = "-"
		}
		lines = append(lines, fmt.Sprintf(
			"scenario=%s status=%s duration=%s failing=%s error=%s final=%s",
			row.ScenarioPath,
			status,
			duration,
			failing,
			errText,
			finalState,
		))
	}
	return strings.Join(lines, "\n")
}

func BuildLastFailure(rows []SummaryRow, transcripts map[string]*transcript.Transcript) *FailureReport {
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		if row.Status != "failed" {
			continue
		}
		tr := transcripts[row.TranscriptLabel]
		if tr == nil {
			return &FailureReport{
				ScenarioPath:     row.ScenarioPath,
				TranscriptLabel:  row.TranscriptLabel,
				Status:           row.Status,
				StartedAt:        row.StartedAt,
				FinishedAt:       row.FinishedAt,
				DurationMS:       row.DurationMS,
				FailingCommandID: row.FailingCommandID,
				FailingAction:    row.FailingAction,
				TerminalError:    row.TerminalError,
				FailureAt:        row.FailureAt,
				LastDiffSummary:  row.LastDiffSummary,
				FinalStateLabel:  row.FinalStateLabel,
				FinalButtons:     row.FinalButtons,
				RawJSON:          row.RawJSON,
				RawText:          row.RawText,
				CompactJSON:      row.CompactJSON,
				CompactText:      row.CompactText,
			}
		}
		report := &FailureReport{
			ScenarioPath:     row.ScenarioPath,
			TranscriptLabel:  row.TranscriptLabel,
			Status:           row.Status,
			StartedAt:        row.StartedAt,
			FinishedAt:       row.FinishedAt,
			DurationMS:       row.DurationMS,
			FailingCommandID: row.FailingCommandID,
			FailingAction:    row.FailingAction,
			TerminalError:    row.TerminalError,
			FailureAt:        row.FailureAt,
			LastDiffSummary:  row.LastDiffSummary,
			RawJSON:          row.RawJSON,
			RawText:          row.RawText,
			CompactJSON:      row.CompactJSON,
			CompactText:      row.CompactText,
		}
		final := summarizeFinalState(lastSnapshot(tr))
		report.FinalStateLabel = final.PrimaryStateLabel
		report.FinalPinnedStateLabel = final.PinnedStateLabel
		report.FinalInteractiveStates = final.InteractiveStateLabels
		report.FinalButtons = final.Buttons
		report.Timeline = buildTimeline(tr)
		report.Window = buildFailureWindow(tr)
		return report
	}
	return nil
}

func (r FailureReport) RenderText() string {
	lines := []string{"Last Failure"}
	lines = append(lines, fmt.Sprintf("scenario=%s", r.ScenarioPath))
	lines = append(lines, fmt.Sprintf("label=%s", r.TranscriptLabel))
	lines = append(lines, fmt.Sprintf("failing=%s:%s", valueOrDash(r.FailingCommandID), valueOrDash(r.FailingAction)))
	lines = append(lines, fmt.Sprintf("error=%s", valueOrDash(r.TerminalError)))
	if !r.FailureAt.IsZero() {
		lines = append(lines, "failure_at="+r.FailureAt.Format(time.RFC3339))
	}
	if r.FinalStateLabel != "" {
		lines = append(lines, "final_state="+r.FinalStateLabel)
	}
	if r.FinalPinnedStateLabel != "" {
		lines = append(lines, "final_pinned="+r.FinalPinnedStateLabel)
	}
	if len(r.FinalInteractiveStates) > 0 {
		lines = append(lines, "final_interactive="+strings.Join(r.FinalInteractiveStates, " | "))
	}
	if len(r.FinalButtons) > 0 {
		lines = append(lines, "final_buttons="+strings.Join(r.FinalButtons, ","))
	}
	if len(r.Timeline) > 0 {
		lines = append(lines, "timeline:")
		for _, entry := range r.Timeline {
			lines = append(lines, fmt.Sprintf("  %s %s %dms", valueOrDash(entry.CommandID), valueOrDash(entry.Action), entry.DurationMS))
		}
	}
	if len(r.Window) > 0 {
		lines = append(lines, "window:")
		for _, entry := range r.Window {
			var parts []string
			parts = append(parts, entry.At.Format(time.RFC3339))
			parts = append(parts, entry.Kind)
			if entry.Type != "" {
				parts = append(parts, "type="+entry.Type)
			}
			if entry.CommandID != "" {
				parts = append(parts, "id="+entry.CommandID)
			}
			if entry.Action != "" {
				parts = append(parts, "action="+entry.Action)
			}
			if entry.Target != "" {
				parts = append(parts, "target="+entry.Target)
			}
			if entry.State != "" {
				parts = append(parts, "state="+entry.State)
			}
			if entry.Diff != "" {
				parts = append(parts, "diff="+entry.Diff)
			}
			if entry.Message != "" {
				parts = append(parts, "msg="+entry.Message)
			}
			if entry.Error != "" {
				parts = append(parts, "error="+entry.Error)
			}
			lines = append(lines, "  "+strings.Join(parts, " "))
		}
	}
	lines = append(lines, "raw_json="+r.RawJSON)
	lines = append(lines, "raw_text="+r.RawText)
	lines = append(lines, "compact_json="+r.CompactJSON)
	lines = append(lines, "compact_text="+r.CompactText)
	return strings.Join(lines, "\n")
}

func MarshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

func compactEvent(event transcript.Event, lastPinnedLabel string) (*CompactEvent, string) {
	entry := CompactEvent{
		At:   event.At,
		Kind: event.Kind,
	}
	switch event.Kind {
	case "command":
		if event.Command == nil {
			return &entry, lastPinnedLabel
		}
		entry.CommandID = event.Command.ID
		entry.Action = event.Command.Action
		entry.Target = normalizeCommandTarget(*event.Command)
		return &entry, lastPinnedLabel
	case "event":
		if event.Output == nil {
			return &entry, lastPinnedLabel
		}
		entry.Type = event.Output.Type
		entry.CommandID = event.Output.CommandID
		entry.Action = event.Output.Action
		if event.Output.Error != "" {
			entry.Error = normalizeError(event.Output.Error)
		}
		if msg := normalizeEventMessage(event.Output); msg != "" {
			entry.Message = msg
		}
		if event.Output.Diff != nil {
			entry.Diff = normalizeDiffSummary(event.Output.Diff.Summary)
		}
		stateLabel := normalizedPrimaryState(event.Output.Snapshot)
		pinnedLabel := normalizedPinnedState(event.Output.Snapshot)
		if stateLabel != "" {
			if entry.Type == "state_update" || entry.Type == "state_snapshot" {
				entry.State = stateLabel
			}
			if pinnedLabel != "" && pinnedLabel == lastPinnedLabel && stateLabel == pinnedLabel {
				entry.State = ""
			}
		}
		if entry.Type == "ack" && entry.Message == "command_executed" {
			entry.Message = ""
		}
		if entry.Type == "info" && entry.Message == "interactive_ready" {
			entry.Message = ""
		}
		if entry.Type == "timeout" && entry.Error == "" && event.Output.Error != "" {
			entry.Error = normalizeError(event.Output.Error)
		}
		if entry.Type == "timeout" && entry.Message == "" {
			entry.Message = "wait_timeout"
		}
		if entry.Type == "state_update" && entry.Message == "visible_state_changed" && entry.Diff == "" {
			entry.Message = ""
		}
		if entry.Type == "state_snapshot" && entry.Message == "chat_snapshot_captured" {
			entry.Message = ""
		}
		if entry.State == "" && entry.Diff == "" && entry.Message == "" && entry.Error == "" && entry.Type == "ack" {
			return &entry, pinnedLabel
		}
		return &entry, pinnedLabel
	default:
		return &entry, lastPinnedLabel
	}
}

func buildTimeline(tr *transcript.Transcript) []FailureTimelineEntry {
	type span struct {
		action     string
		startedAt  time.Time
		finishedAt time.Time
	}
	spans := map[string]*span{}
	order := make([]string, 0)
	for _, event := range tr.Events {
		switch event.Kind {
		case "command":
			if event.Command == nil {
				continue
			}
			id := event.Command.ID
			if id == "" {
				continue
			}
			if _, ok := spans[id]; !ok {
				order = append(order, id)
				spans[id] = &span{action: event.Command.Action, startedAt: event.At, finishedAt: event.At}
				continue
			}
			spans[id].finishedAt = event.At
		case "event":
			if event.Output == nil || event.Output.CommandID == "" {
				continue
			}
			id := event.Output.CommandID
			if _, ok := spans[id]; !ok {
				order = append(order, id)
				spans[id] = &span{action: event.Output.Action, startedAt: event.At, finishedAt: event.At}
				continue
			}
			if spans[id].action == "" {
				spans[id].action = event.Output.Action
			}
			spans[id].finishedAt = event.At
		}
	}
	out := make([]FailureTimelineEntry, 0, len(order))
	for _, id := range order {
		span := spans[id]
		if span == nil {
			continue
		}
		out = append(out, FailureTimelineEntry{
			CommandID:  id,
			Action:     span.action,
			StartedAt:  span.startedAt,
			FinishedAt: span.finishedAt,
			DurationMS: span.finishedAt.Sub(span.startedAt).Milliseconds(),
		})
	}
	return out
}

func buildFailureWindow(tr *transcript.Transcript) []CompactEvent {
	idx := -1
	for i := len(tr.Events) - 1; i >= 0; i-- {
		event := tr.Events[i]
		if event.Kind == "event" && event.Output != nil && (event.Output.Type == "error" || event.Output.Type == "timeout") {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil
	}
	start := idx - (failureWindowSize - maxFailureWindowAfter - 1)
	if start < 0 {
		start = 0
	}
	end := idx + maxFailureWindowAfter + 1
	if end > len(tr.Events) {
		end = len(tr.Events)
	}
	window := make([]CompactEvent, 0, end-start)
	var lastPinned string
	if start > 0 {
		for i := 0; i < start; i++ {
			if tr.Events[i].Kind == "event" && tr.Events[i].Output != nil && tr.Events[i].Output.Snapshot != nil {
				if pinned := normalizedPinnedState(tr.Events[i].Output.Snapshot); pinned != "" {
					lastPinned = pinned
				}
			}
		}
	}
	for _, event := range tr.Events[start:end] {
		entry, pinned := compactEvent(event, lastPinned)
		if entry != nil {
			window = append(window, *entry)
		}
		if pinned != "" {
			lastPinned = pinned
		}
	}
	return window
}

func terminalFailure(tr *transcript.Transcript) *CompactEvent {
	for i := len(tr.Events) - 1; i >= 0; i-- {
		event := tr.Events[i]
		if event.Kind != "event" || event.Output == nil {
			continue
		}
		if event.Output.Type != "error" && event.Output.Type != "timeout" {
			continue
		}
		entry, _ := compactEvent(event, "")
		return entry
	}
	return nil
}

func lastDiffSummary(tr *transcript.Transcript) string {
	for i := len(tr.Events) - 1; i >= 0; i-- {
		event := tr.Events[i]
		if event.Kind != "event" || event.Output == nil || event.Output.Diff == nil {
			continue
		}
		if summary := normalizeDiffSummary(event.Output.Diff.Summary); summary != "" {
			return summary
		}
	}
	return ""
}

func lastSnapshot(tr *transcript.Transcript) *state.ChatState {
	for i := len(tr.Events) - 1; i >= 0; i-- {
		event := tr.Events[i]
		if event.Kind != "event" || event.Output == nil || event.Output.Snapshot == nil {
			continue
		}
		return event.Output.Snapshot
	}
	return nil
}

func transcriptFinishedAt(tr *transcript.Transcript) time.Time {
	if len(tr.Events) == 0 {
		return time.Time{}
	}
	return tr.Events[len(tr.Events)-1].At
}

func summarizeFinalState(snapshot *state.ChatState) finalStateSummary {
	var summary finalStateSummary
	if snapshot == nil {
		return summary
	}
	summary.PinnedStateLabel = normalizedPinnedState(snapshot)
	interactive := interactiveStateLabels(snapshot)
	summary.InteractiveStateLabels = interactive
	summary.PrimaryStateLabel = summary.PinnedStateLabel
	if len(interactive) > 0 {
		summary.PrimaryStateLabel = interactive[0]
	}
	summary.Buttons = collectFinalButtons(snapshot)
	return summary
}

func interactiveStateLabels(snapshot *state.ChatState) []string {
	if snapshot == nil {
		return nil
	}
	out := make([]string, 0, maxFailureStates)
	for i := len(snapshot.Messages) - 1; i >= 0; i-- {
		msg := snapshot.Messages[i]
		if len(msg.Buttons) == 0 || msg.Sender != "bot" || msg.Pinned {
			continue
		}
		label := normalizeStateLabel(msg.Text)
		if label == "" || label == normalizedPinnedState(snapshot) || slices.Contains(out, label) {
			continue
		}
		out = append(out, label)
		if len(out) == maxFailureStates {
			break
		}
	}
	return out
}

func normalizedPrimaryState(snapshot *state.ChatState) string {
	final := summarizeFinalState(snapshot)
	return final.PrimaryStateLabel
}

func normalizedPinnedState(snapshot *state.ChatState) string {
	if snapshot == nil || snapshot.Pinned == nil {
		return ""
	}
	return normalizeStateLabel(snapshot.Pinned.Text)
}

func collectFinalButtons(snapshot *state.ChatState) []string {
	if snapshot == nil {
		return nil
	}
	out := make([]string, 0, maxFinalButtons)
	addRows := func(rows [][]state.InlineButton) {
		for _, row := range rows {
			for _, button := range row {
				label := normalizeButtonLabel(button.Text)
				if label == "" || slices.Contains(out, label) {
					continue
				}
				out = append(out, label)
				if len(out) == maxFinalButtons {
					return
				}
			}
			if len(out) == maxFinalButtons {
				return
			}
		}
	}
	for i := len(snapshot.Messages) - 1; i >= 0 && len(out) < maxFinalButtons; i-- {
		msg := snapshot.Messages[i]
		if len(msg.Buttons) == 0 || msg.Sender != "bot" || msg.Pinned {
			continue
		}
		addRows(msg.Buttons)
	}
	if len(out) < maxFinalButtons && snapshot.Pinned != nil {
		addRows(snapshot.Pinned.Buttons)
	}
	return out
}

func normalizeCommandTarget(cmd protocol.Command) string {
	switch cmd.Action {
	case "send_text":
		return "text=" + compactExcerpt(cmd.Text, messageExcerptLimit)
	case "send_photo", "send_voice", "send_audio", "send_document":
		return "path=" + compactExcerpt(cmd.Path, messageExcerptLimit)
	case "click_button":
		target := "button=" + normalizeButtonLabel(cmd.ButtonText)
		if cmd.MessageOffset > 0 {
			target += fmt.Sprintf(" offset=%d", cmd.MessageOffset)
		}
		return target
	case "wait":
		if cmd.TimeoutMS > 0 {
			return fmt.Sprintf("timeout_ms=%d", cmd.TimeoutMS)
		}
		return ""
	default:
		return ""
	}
}

func normalizeEventMessage(evt *protocol.Event) string {
	if evt == nil {
		return ""
	}
	message := collapseWhitespace(evt.Message)
	switch message {
	case "":
		return ""
	case "chat selected":
		return "chat_selected"
	case "command executed":
		return "command_executed"
	case "chat snapshot captured":
		return "chat_snapshot_captured"
	case "visible chat state changed":
		return "visible_state_changed"
	case "visible chat state already changed after previous action":
		return "pending_wait_reused"
	case "waiting for visible chat changes":
		return "waiting"
	default:
		return compactExcerpt(message, messageExcerptLimit)
	}
}

func normalizeDiffSummary(summary string) string {
	summary = collapseWhitespace(summary)
	if summary == "" || summary == "no visible changes" {
		return ""
	}
	return summary
}

func normalizeError(err string) string {
	return compactExcerpt(err, messageExcerptLimit)
}

func normalizeStateLabel(text string) string {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return ""
	}
	switch {
	case strings.Contains(raw, "Главная") && reHomeTracked.MatchString(raw):
		return fmt.Sprintf(
			"home tracked=%s soon=%s expired=%s",
			firstSubmatch(reHomeTracked, raw),
			firstSubmatch(reHomeSoon, raw),
			firstSubmatch(reHomeExpired, raw),
		)
	case strings.Contains(raw, "Настройки") && reSettingsTZ.MatchString(raw):
		return fmt.Sprintf(
			"settings tz=%s digest=%s",
			collapseWhitespace(firstSubmatch(reSettingsTZ, raw)),
			firstSubmatch(reSettingsDigest, raw),
		)
	case strings.Contains(raw, "Статистика") && reStatsEaten.MatchString(raw):
		return fmt.Sprintf(
			"stats tracked=%s soon=%s expired=%s eaten=%s wasted=%s deleted=%s",
			firstSubmatch(reHomeTracked, raw),
			firstSubmatch(reHomeSoon, raw),
			firstSubmatch(reHomeExpired, raw),
			firstSubmatch(reStatsEaten, raw),
			firstSubmatch(reStatsWasted, raw),
			firstSubmatch(reStatsDeleted, raw),
		)
	case strings.Contains(raw, "Активные продукты"):
		if strings.Count(raw, "•") == 0 {
			return "list_empty"
		}
		return fmt.Sprintf("list count=%d", strings.Count(raw, "•"))
	case strings.Contains(raw, "Скоро истекают") && strings.Count(raw, "•") >= 0 && !strings.Contains(raw, "Сейчас отслеживаем"):
		if strings.Count(raw, "•") == 0 {
			return "soon_empty"
		}
		return fmt.Sprintf("soon count=%d", strings.Count(raw, "•"))
	case strings.Contains(raw, "Новый продукт"):
		name := collapseWhitespace(firstSubmatch(reDraftName, raw))
		date := collapseWhitespace(firstSubmatch(reDraftDate, raw))
		source := collapseWhitespace(firstSubmatch(reDraftSource, raw))
		if date == "" || strings.Contains(strings.ToLower(date), "не указан") {
			date = "missing"
		}
		status := "ready"
		lower := strings.ToLower(raw)
		switch {
		case strings.Contains(lower, "укажи срок"):
			status = "needs_date"
		case strings.Contains(lower, "укажи название"):
			status = "needs_name"
		}
		return fmt.Sprintf("draft name=%s date=%s source=%s status=%s",
			safeField(name),
			safeField(date),
			safeField(source),
			status,
		)
	case strings.Contains(strings.ToLower(raw), "укажи срок"):
		return "prompt_date"
	case strings.Contains(strings.ToLower(raw), "укажи название"):
		return "prompt_name"
	case strings.Contains(strings.ToLower(raw), "принял") || strings.Contains(strings.ToLower(raw), "обрабаты"):
		return "processing"
	case strings.Contains(strings.ToLower(raw), "отмен"):
		return "feedback_cancelled"
	case strings.Contains(strings.ToLower(raw), "невер") || strings.Contains(strings.ToLower(raw), "не смог распознать дату"):
		return "feedback_invalid_date"
	case strings.Contains(strings.ToLower(raw), "удален") || strings.Contains(strings.ToLower(raw), "удалён"):
		return "feedback_deleted"
	case strings.Contains(strings.ToLower(raw), "сохран"):
		return "feedback_saved"
	default:
		return compactExcerpt(raw, messageExcerptLimit)
	}
}

func normalizeButtonLabel(text string) string {
	trimmed := collapseWhitespace(text)
	switch trimmed {
	case "📋 Список", "Список":
		return "list"
	case "⏰ Скоро", "Скоро":
		return "soon"
	case "📊 Статистика", "Статистика":
		return "stats"
	case "⚙️ Настройки", "Настройки":
		return "settings"
	case "⬅️ Назад", "Назад":
		return "back"
	case "✅ Сохранить", "Сохранить":
		return "save"
	case "📝 Название", "Название":
		return "edit_name"
	case "📅 Срок", "Срок":
		return "edit_date"
	case "❌ Удалить", "Удалить":
		return "delete"
	case "↩️ Отмена", "Отмена":
		return "cancel"
	}
	plain := compactExcerpt(trimmed, 32)
	if plain == "" {
		return ""
	}
	return plain
}

func compactExcerpt(value string, limit int) string {
	value = collapseWhitespace(stripDecorativeSymbols(value))
	if value == "" {
		return "-"
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func stripDecorativeSymbols(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case unicode.Is(unicode.So, r):
			continue
		case unicode.Is(unicode.Sk, r):
			continue
		case unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func collapseWhitespace(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	value = reWhitespace.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func firstSubmatch(re *regexp.Regexp, value string) string {
	if re == nil {
		return ""
	}
	matches := re.FindStringSubmatch(value)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func safeField(value string) string {
	value = compactExcerpt(value, 32)
	if value == "" || value == "-" {
		return "missing"
	}
	return value
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func SortRows(rows []SummaryRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].StartedAt.Equal(rows[j].StartedAt) {
			return rows[i].ScenarioPath < rows[j].ScenarioPath
		}
		return rows[i].StartedAt.Before(rows[j].StartedAt)
	})
}

func NormalizeLogLine(line string) string {
	line = collapseWhitespace(line)
	if line == "" {
		return ""
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		return compactExcerpt(line, 200)
	}
	keys := []string{"time", "level", "msg", "trace_id", "update_id", "job_id", "error"}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value, ok := decoded[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) == "" {
				continue
			}
			parts = append(parts, key+"="+compactExcerpt(typed, 120))
		case float64:
			parts = append(parts, key+"="+strconv.FormatInt(int64(typed), 10))
		default:
			parts = append(parts, key+"="+compactExcerpt(fmt.Sprint(typed), 120))
		}
	}
	return strings.Join(parts, " ")
}
