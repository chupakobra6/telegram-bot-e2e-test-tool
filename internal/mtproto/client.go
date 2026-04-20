package mtproto

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/config"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/state"
)

type Client struct {
	cfg config.Config
}

type Session struct {
	raw    *tg.Client
	sender *message.Sender
	cache  map[string]resolvedTarget
}

type resolvedTarget struct {
	Raw       string
	Display   string
	PeerID    int64
	InputPeer tg.InputPeerClass
}

func New(cfg config.Config) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Login(ctx context.Context, in *os.File, out *os.File) error {
	if err := c.cfg.ValidateLogin(); err != nil {
		return err
	}
	if err := ensureSessionDir(c.cfg.SessionPath); err != nil {
		return err
	}
	client := c.newTelegramClient()
	reader := bufio.NewReader(in)
	_, _ = fmt.Fprintf(out, "starting MTProto login for %s\n", maskPhone(c.cfg.Phone))
	_, _ = fmt.Fprintln(out, "connecting to Telegram...")
	return client.Run(ctx, func(runCtx context.Context) error {
		status, err := client.Auth().Status(runCtx)
		if err != nil {
			return fmt.Errorf("auth status: %w", err)
		}
		if status.Authorized {
			_, _ = fmt.Fprintln(out, "session already authorized")
			return nil
		}

		_, _ = fmt.Fprintf(out, "connected, authenticating as %s\n", maskPhone(c.cfg.Phone))
		_, _ = fmt.Fprintln(out, "requesting login code...")
		sentCodeClass, err := client.Auth().SendCode(runCtx, c.cfg.Phone, auth.SendCodeOptions{})
		if err != nil {
			return fmt.Errorf("send code: %w", err)
		}

		sentCode, ok := sentCodeClass.(*tg.AuthSentCode)
		if !ok {
			if success, ok := sentCodeClass.(*tg.AuthSentCodeSuccess); ok {
				_ = success
				_, _ = fmt.Fprintln(out, "login successful")
				return nil
			}
			return fmt.Errorf("unexpected sent code type %T", sentCodeClass)
		}

		_, _ = fmt.Fprintf(out, "code requested via %s\n", sentCodeTypeSummary(sentCode))
		code, err := promptLine(out, reader, "code: ")
		if err != nil {
			return fmt.Errorf("read code: %w", err)
		}

		if _, err := client.Auth().SignIn(runCtx, c.cfg.Phone, code, sentCode.PhoneCodeHash); err != nil {
			if errors.Is(err, auth.ErrPasswordAuthNeeded) {
				password := strings.TrimSpace(c.cfg.Password)
				if password == "" {
					_, _ = fmt.Fprintln(out, "two-factor authentication is enabled")
					password, err = promptLine(out, reader, "password: ")
					if err != nil {
						return fmt.Errorf("read password: %w", err)
					}
				}
				if _, err := client.Auth().Password(runCtx, password); err != nil {
					return fmt.Errorf("sign in with password: %w", err)
				}
			} else {
				return fmt.Errorf("sign in: %w", err)
			}
		}

		_, _ = fmt.Fprintln(out, "login successful")
		return nil
	})
}

func (c *Client) RunAuthorized(ctx context.Context, fn func(context.Context, *Session) error) error {
	if err := c.cfg.ValidateRuntime(); err != nil {
		return err
	}
	if err := ensureSessionDir(c.cfg.SessionPath); err != nil {
		return err
	}
	client := c.newTelegramClient()
	return client.Run(ctx, func(runCtx context.Context) error {
		status, err := client.Auth().Status(runCtx)
		if err != nil {
			return fmt.Errorf("auth status: %w", err)
		}
		if !status.Authorized {
			return fmt.Errorf("telegram session is not authorized; run `tg-e2e-tool login` first")
		}
		session := &Session{
			raw:    tg.NewClient(client),
			sender: message.NewSender(tg.NewClient(client)),
			cache:  map[string]resolvedTarget{},
		}
		return fn(runCtx, session)
	})
}

func (s *Session) SendText(ctx context.Context, chat string, text string) error {
	target, err := s.resolveTarget(ctx, chat)
	if err != nil {
		return err
	}
	_, err = s.sender.To(target.InputPeer).Text(ctx, text)
	return err
}

func (s *Session) SendPhoto(ctx context.Context, chat string, path string, caption string) error {
	target, err := s.resolveTarget(ctx, chat)
	if err != nil {
		return err
	}
	builder := s.sender.To(target.InputPeer).Upload(message.FromPath(path))
	if strings.TrimSpace(caption) == "" {
		_, err = builder.Photo(ctx)
	} else {
		_, err = builder.Photo(ctx, styling.Plain(caption))
	}
	return err
}

func (s *Session) SendVoice(ctx context.Context, chat string, path string) error {
	target, err := s.resolveTarget(ctx, chat)
	if err != nil {
		return err
	}
	_, err = s.sender.To(target.InputPeer).Upload(message.FromPath(path)).Voice(ctx)
	return err
}

func (s *Session) SendAudio(ctx context.Context, chat string, path string) error {
	target, err := s.resolveTarget(ctx, chat)
	if err != nil {
		return err
	}
	_, err = s.sender.To(target.InputPeer).Upload(message.FromPath(path)).Audio(ctx)
	return err
}

func (s *Session) ClickButton(ctx context.Context, chat string, messageID int, data []byte) error {
	target, err := s.resolveTarget(ctx, chat)
	if err != nil {
		return err
	}
	_, err = s.raw.MessagesGetBotCallbackAnswer(ctx, &tg.MessagesGetBotCallbackAnswerRequest{
		Peer:  target.InputPeer,
		MsgID: messageID,
		Data:  data,
	})
	return err
}

func (s *Session) SyncChat(ctx context.Context, chat string, limit int) (state.ChatState, error) {
	target, err := s.resolveTarget(ctx, chat)
	if err != nil {
		return state.ChatState{}, err
	}
	result, err := s.raw.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:       target.InputPeer,
		OffsetID:   0,
		OffsetDate: 0,
		AddOffset:  0,
		Limit:      limit,
		MaxID:      0,
		MinID:      0,
		Hash:       0,
	})
	if err != nil {
		return state.ChatState{}, err
	}
	entities := historyEntities(result)
	messages := historyMessages(result)
	visible := make([]state.VisibleMessage, 0, len(messages))
	for _, msgClass := range messages {
		switch msg := msgClass.(type) {
		case *tg.Message:
			normalized := normalizeMessage(*msg, entities)
			visible = append(visible, normalized)
		case *tg.MessageService:
			normalized := normalizeServiceMessage(*msg, entities)
			visible = append(visible, normalized)
		}
	}
	pinned, err := s.lookupPinned(ctx, target, visible)
	if err != nil {
		return state.ChatState{}, err
	}
	if pinned != nil {
		for i := range visible {
			if visible[i].ID == pinned.MessageID {
				visible[i].Pinned = true
				break
			}
		}
	}
	sort.Slice(visible, func(i, j int) bool { return visible[i].ID < visible[j].ID })
	return state.ChatState{
		Target:     target.Raw,
		ResolvedAs: target.Display,
		PeerID:     target.PeerID,
		SyncedAt:   time.Now().UTC(),
		Messages:   visible,
		Pinned:     pinned,
	}, nil
}

func (s *Session) resolveTarget(ctx context.Context, raw string) (resolvedTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return resolvedTarget{}, fmt.Errorf("target chat is empty")
	}
	if cached, ok := s.cache[raw]; ok {
		return cached, nil
	}
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
		target, err := s.resolveNumeric(ctx, id, raw)
		if err != nil {
			return resolvedTarget{}, err
		}
		s.cache[raw] = target
		return target, nil
	}

	username := strings.TrimPrefix(raw, "@")
	resolved, err := s.raw.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
		Username: username,
	})
	if err != nil {
		return resolvedTarget{}, fmt.Errorf("resolve username %q: %w", raw, err)
	}
	entities := peer.EntitiesFromResult(resolved)
	inputPeer, err := entities.ExtractPeer(resolved.Peer)
	if err != nil {
		return resolvedTarget{}, err
	}

	target := resolvedTarget{
		Raw:       raw,
		Display:   "@" + username,
		InputPeer: inputPeer,
		PeerID:    peerClassID(resolved.Peer),
	}
	s.cache[raw] = target
	s.cache[strconv.FormatInt(target.PeerID, 10)] = target
	return target, nil
}

func (s *Session) resolveNumeric(ctx context.Context, id int64, raw string) (resolvedTarget, error) {
	if cached, ok := s.cache[strconv.FormatInt(id, 10)]; ok {
		return cached, nil
	}
	entities, err := s.loadAllDialogEntities(ctx)
	if err != nil {
		return resolvedTarget{}, err
	}
	if user, ok := entities.User(id); ok {
		target := resolvedTarget{
			Raw:       raw,
			Display:   usernameOrID(user.Username, id),
			InputPeer: user.AsInputPeer(),
			PeerID:    id,
		}
		s.cache[raw] = target
		return target, nil
	}
	if chat, ok := entities.Chat(id); ok {
		target := resolvedTarget{
			Raw:       raw,
			Display:   chat.Title,
			InputPeer: &tg.InputPeerChat{ChatID: id},
			PeerID:    id,
		}
		s.cache[raw] = target
		return target, nil
	}
	if channel, ok := entities.Channel(id); ok {
		target := resolvedTarget{
			Raw:       raw,
			Display:   usernameOrID(channel.Username, id),
			InputPeer: &tg.InputPeerChannel{ChannelID: id, AccessHash: channel.AccessHash},
			PeerID:    id,
		}
		s.cache[raw] = target
		return target, nil
	}
	return resolvedTarget{}, fmt.Errorf("numeric peer id %d was not found in known dialogs; use @username first", id)
}

func normalizeMessage(msg tg.Message, entities peer.Entities) state.VisibleMessage {
	sender, senderID := messageSender(msg.Out, msg.GetFromID, entities)
	return state.VisibleMessage{
		ID:        msg.ID,
		Sender:    sender,
		SenderID:  senderID,
		Outgoing:  msg.Out,
		Kind:      normalizeKind(msg.Media),
		Text:      messageText(msg),
		Pinned:    msg.Pinned,
		Timestamp: time.Unix(int64(msg.Date), 0).UTC(),
		Buttons:   extractButtons(msg),
	}
}

func normalizeServiceMessage(msg tg.MessageService, entities peer.Entities) state.VisibleMessage {
	sender, senderID := messageSender(msg.Out, msg.GetFromID, entities)
	return state.VisibleMessage{
		ID:        msg.ID,
		Sender:    sender,
		SenderID:  senderID,
		Outgoing:  msg.Out,
		Kind:      "service",
		Text:      serviceActionText(msg.Action),
		Timestamp: time.Unix(int64(msg.Date), 0).UTC(),
	}
}

func messageSender(out bool, getFromID func() (tg.PeerClass, bool), entities peer.Entities) (string, int64) {
	sender := "peer"
	var senderID int64
	if out {
		sender = "self"
	}
	if from, ok := getFromID(); ok {
		senderID = peerClassID(from)
		if !out {
			if user, ok := extractUser(entities, from); ok && user.Bot {
				sender = "bot"
			}
		}
	}
	return sender, senderID
}

func messageText(msg tg.Message) string {
	if text := strings.TrimSpace(msg.Message); text != "" {
		return text
	}
	switch msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		return "[photo]"
	case *tg.MessageMediaDocument:
		return documentPlaceholder(msg.Media)
	case *tg.MessageMediaUnsupported:
		return "[unsupported media]"
	default:
		return ""
	}
}

func serviceActionText(action tg.MessageActionClass) string {
	switch typed := action.(type) {
	case *tg.MessageActionPinMessage:
		return "[service] message pinned"
	case nil:
		return "[service]"
	default:
		return "[service] " + typed.TypeName()
	}
}

func historyEntities(result tg.MessagesMessagesClass) peer.Entities {
	modified, ok := result.AsModified()
	if !ok {
		return peer.Entities{}
	}
	chats := tg.ChatClassArray(modified.GetChats())
	return peer.NewEntities(
		tg.UserClassArray(modified.GetUsers()).UserToMap(),
		chats.ChatToMap(),
		chats.ChannelToMap(),
	)
}

func historyMessages(result tg.MessagesMessagesClass) []tg.MessageClass {
	modified, ok := result.AsModified()
	if !ok {
		return nil
	}
	return modified.GetMessages()
}

func dialogEntities(result tg.MessagesDialogsClass) peer.Entities {
	modified, ok := result.AsModified()
	if !ok {
		return peer.Entities{}
	}
	chats := tg.ChatClassArray(modified.GetChats())
	return peer.NewEntities(
		tg.UserClassArray(modified.GetUsers()).UserToMap(),
		chats.ChatToMap(),
		chats.ChannelToMap(),
	)
}

func normalizeKind(media tg.MessageMediaClass) string {
	switch typed := media.(type) {
	case *tg.MessageMediaPhoto:
		return "photo"
	case *tg.MessageMediaDocument:
		return documentKind(typed)
	case nil:
		return "text"
	default:
		return "media"
	}
}

func documentKind(media *tg.MessageMediaDocument) string {
	if media == nil {
		return "document"
	}
	if media.Voice {
		return "voice"
	}
	if media.Round {
		return "round_video"
	}
	if media.Video {
		return "video"
	}
	if document, ok := media.GetDocument(); ok {
		if doc, ok := document.(*tg.Document); ok {
			for _, attr := range doc.Attributes {
				audio, ok := attr.(*tg.DocumentAttributeAudio)
				if !ok {
					continue
				}
				if audio.Voice {
					return "voice"
				}
				return "audio"
			}
		}
	}
	return "document"
}

func documentPlaceholder(media tg.MessageMediaClass) string {
	typed, ok := media.(*tg.MessageMediaDocument)
	if !ok {
		return "[document]"
	}
	switch documentKind(typed) {
	case "voice":
		return "[voice]"
	case "audio":
		return "[audio]"
	case "video":
		return "[video]"
	case "round_video":
		return "[round video]"
	default:
		return "[document]"
	}
}

func extractButtons(msg tg.Message) [][]state.InlineButton {
	markup, ok := msg.GetReplyMarkup()
	if !ok {
		return nil
	}
	inlineMarkup, ok := markup.(*tg.ReplyInlineMarkup)
	if !ok {
		return nil
	}
	rows := make([][]state.InlineButton, 0, len(inlineMarkup.Rows))
	for _, row := range inlineMarkup.Rows {
		buttons := make([]state.InlineButton, 0, len(row.Buttons))
		for _, button := range row.Buttons {
			switch typed := button.(type) {
			case *tg.KeyboardButtonCallback:
				buttons = append(buttons, state.InlineButton{
					Text:         typed.Text,
					Kind:         "callback",
					CallbackData: base64.StdEncoding.EncodeToString(typed.Data),
				})
			case *tg.KeyboardButtonURL:
				buttons = append(buttons, state.InlineButton{
					Text: typed.Text,
					Kind: "url",
				})
			default:
				buttons = append(buttons, state.InlineButton{
					Text: buttonLabel(button),
					Kind: "unsupported",
				})
			}
		}
		rows = append(rows, buttons)
	}
	return rows
}

func buttonLabel(button tg.KeyboardButtonClass) string {
	switch typed := button.(type) {
	case interface{ GetText() string }:
		return typed.GetText()
	default:
		return fmt.Sprintf("%T", button)
	}
}

func extractUser(entities peer.Entities, from tg.PeerClass) (*tg.User, bool) {
	userPeer, ok := from.(*tg.PeerUser)
	if !ok {
		return nil, false
	}
	return entities.User(userPeer.UserID)
}

func peerClassID(peerClass tg.PeerClass) int64 {
	switch peer := peerClass.(type) {
	case *tg.PeerUser:
		return peer.UserID
	case *tg.PeerChat:
		return peer.ChatID
	case *tg.PeerChannel:
		return peer.ChannelID
	default:
		return 0
	}
}

func usernameOrID(username string, id int64) string {
	if strings.TrimSpace(username) != "" {
		return "@" + username
	}
	return strconv.FormatInt(id, 10)
}

func ensureSessionDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func (c *Client) newTelegramClient() *telegram.Client {
	return telegram.NewClient(c.cfg.AppID, c.cfg.AppHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: c.cfg.SessionPath},
		Resolver:       dcs.Plain(dcs.PlainOptions{Dial: proxyAwareDialContext}),
	})
}

func promptLine(out *os.File, reader *bufio.Reader, label string) (string, error) {
	_, _ = fmt.Fprint(out, label)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func maskPhone(phone string) string {
	trimmed := strings.TrimSpace(phone)
	if len(trimmed) <= 4 {
		return trimmed
	}
	return trimmed[:2] + strings.Repeat("*", max(0, len(trimmed)-4)) + trimmed[len(trimmed)-2:]
}

func sentCodeTypeSummary(sentCode *tg.AuthSentCode) string {
	switch sentCode.Type.(type) {
	case *tg.AuthSentCodeTypeApp:
		return "Telegram app"
	case *tg.AuthSentCodeTypeSMS:
		return "SMS"
	case *tg.AuthSentCodeTypeCall:
		return "phone call"
	case *tg.AuthSentCodeTypeFlashCall:
		return "flash call"
	case *tg.AuthSentCodeTypeMissedCall:
		return "missed call"
	case *tg.AuthSentCodeTypeEmailCode:
		return "email"
	case *tg.AuthSentCodeTypeSetUpEmailRequired:
		return "email setup required"
	case *tg.AuthSentCodeTypeFragmentSMS:
		return "fragment SMS"
	default:
		return fmt.Sprintf("%T", sentCode.Type)
	}
}

func (s *Session) lookupPinned(ctx context.Context, target resolvedTarget, visible []state.VisibleMessage) (*state.PinnedMessage, error) {
	for _, msg := range visible {
		if msg.Pinned {
			return &state.PinnedMessage{
				MessageID: msg.ID,
				Text:      msg.Text,
			}, nil
		}
	}

	result, err := s.raw.MessagesSearch(ctx, &tg.MessagesSearchRequest{
		Peer:   target.InputPeer,
		Q:      "",
		Filter: &tg.InputMessagesFilterPinned{},
		Limit:  10,
	})
	if err != nil {
		return nil, fmt.Errorf("load pinned messages: %w", err)
	}

	entities := historyEntities(result)
	var newest *state.VisibleMessage
	for _, msgClass := range historyMessages(result) {
		msg, ok := msgClass.(*tg.Message)
		if !ok {
			continue
		}
		normalized := normalizeMessage(*msg, entities)
		if newest == nil || normalized.ID > newest.ID {
			copy := normalized
			newest = &copy
		}
	}
	if newest == nil {
		return nil, nil
	}
	return &state.PinnedMessage{
		MessageID: newest.ID,
		Text:      newest.Text,
	}, nil
}

func (s *Session) loadAllDialogEntities(ctx context.Context) (peer.Entities, error) {
	combined := peer.Entities{}
	offsetPeer := tg.InputPeerClass(&tg.InputPeerEmpty{})
	offsetID := 0
	offsetDate := 0

	for {
		result, err := s.raw.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
			ExcludePinned: false,
			OffsetDate:    offsetDate,
			OffsetID:      offsetID,
			OffsetPeer:    offsetPeer,
			Limit:         100,
			Hash:          0,
		})
		if err != nil {
			return peer.Entities{}, fmt.Errorf("load dialogs: %w", err)
		}

		modified, ok := result.AsModified()
		if !ok {
			break
		}
		chats := tg.ChatClassArray(modified.GetChats())
		chunk := peer.NewEntities(
			tg.UserClassArray(modified.GetUsers()).UserToMap(),
			chats.ChatToMap(),
			chats.ChannelToMap(),
		)
		combined.Fill(chunk.Users(), chunk.Chats(), chunk.Channels())

		messages := modified.GetMessages()
		if len(messages) == 0 {
			break
		}
		lastMessage, ok := messages[len(messages)-1].(*tg.Message)
		if !ok {
			break
		}
		offsetID = lastMessage.ID
		offsetDate = lastMessage.Date
		offsetPeer = inputPeerFromMessagePeer(lastMessage.PeerID, combined)
		if offsetPeer == nil || len(messages) < 100 {
			break
		}
	}

	return combined, nil
}

func inputPeerFromMessagePeer(peerClass tg.PeerClass, entities peer.Entities) tg.InputPeerClass {
	switch typed := peerClass.(type) {
	case *tg.PeerUser:
		if user, ok := entities.User(typed.UserID); ok {
			return user.AsInputPeer()
		}
	case *tg.PeerChat:
		return &tg.InputPeerChat{ChatID: typed.ChatID}
	case *tg.PeerChannel:
		if channel, ok := entities.Channel(typed.ChannelID); ok {
			return &tg.InputPeerChannel{ChannelID: typed.ChannelID, AccessHash: channel.AccessHash}
		}
	}
	return nil
}
