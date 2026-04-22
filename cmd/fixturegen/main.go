package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/fixturegen"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("fixturegen", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	output := fs.String("output", "", "path to the generated PNG fixture")
	preset := fs.String("preset", "package", "fixture preset: package, receipt, blank")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *output == "" {
		return fmt.Errorf("--output is required")
	}
	return fixturegen.WritePNG(*output, *preset)
}
