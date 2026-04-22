package textcase

import "github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"

func Render(targetChat, text, cancelButton string, waitTimeoutMS int) []protocol.Command {
	commands := []protocol.Command{
		{ID: "select_chat", Action: "select_chat", Chat: targetChat},
		{ID: "send_case", Action: "send_text", Text: text},
		{ID: "wait_case", Action: "wait", TimeoutMS: waitTimeoutMS},
		{ID: "dump_case", Action: "dump_state"},
	}
	if cancelButton != "" {
		commands = append(commands,
			protocol.Command{ID: "cancel_case", Action: "click_button", ButtonText: cancelButton},
			protocol.Command{ID: "wait_cancel", Action: "wait", TimeoutMS: waitTimeoutMS},
		)
	}
	return commands
}
