package scenario

import (
	"fmt"
	"os"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
)

func Load(path string) ([]protocol.Command, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open scenario: %w", err)
	}
	defer file.Close()

	commands := make([]protocol.Command, 0, 16)
	if err := protocol.ReadCommands(file, func(cmd protocol.Command) error {
		commands = append(commands, cmd)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read scenario: %w", err)
	}
	return commands, nil
}
