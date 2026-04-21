package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

type command struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Chat      string `json:"chat,omitempty"`
	Text      string `json:"text,omitempty"`
	Button    string `json:"button_text,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "render-text-case":
		if err := runRenderTextCase(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "help", "--help", "-h":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
}

func runRenderTextCase(args []string) error {
	fs := flag.NewFlagSet("render-text-case", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	output := fs.String("output", "", "path to the generated JSONL scenario")
	text := fs.String("text", "", "raw user text to send to the bot")
	cancelButton := fs.String("cancel-button", "↩️ Отмена", "cleanup button text for the transient draft")
	waitTimeoutMS := fs.Int("wait-timeout-ms", 12000, "timeout for wait actions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *output == "" {
		return fmt.Errorf("--output is required")
	}
	if *text == "" {
		return fmt.Errorf("--text is required")
	}

	commands := []command{
		{ID: "select_chat", Action: "select_chat", Chat: "@your_bot_username"},
		{ID: "send_case", Action: "send_text", Text: *text},
		{ID: "wait_case", Action: "wait", TimeoutMS: *waitTimeoutMS},
		{ID: "dump_case", Action: "dump_state"},
	}
	if *cancelButton != "" {
		commands = append(commands,
			command{ID: "cancel_case", Action: "click_button", Button: *cancelButton},
			command{ID: "wait_cancel", Action: "wait", TimeoutMS: *waitTimeoutMS},
		)
	}

	file, err := os.Create(*output)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetEscapeHTML(false)
	for _, cmd := range commands {
		if err := enc.Encode(cmd); err != nil {
			return fmt.Errorf("encode command %s: %w", cmd.ID, err)
		}
	}
	return nil
}

func usage(out *os.File) {
	fmt.Fprintln(out, "usage: scenario-helper render-text-case --output /tmp/case.jsonl --text \"кефир до пятницы\" [--cancel-button \"↩️ Отмена\"]")
}
