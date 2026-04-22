package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/fixturegen"
)

func runFixturesCommand(repoRoot string) error {
	outDir := strings.TrimSpace(os.Getenv("TG_E2E_FIXTURE_DIR"))
	if outDir == "" {
		outDir = filepath.Join(repoRoot, "artifacts", "fixtures")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg is required to generate audio fixtures")
	}

	if err := fixturegen.WritePNG(filepath.Join(outDir, fixturegen.PhotoFixtureName), "package"); err != nil {
		return err
	}
	if err := fixturegen.WritePNG(filepath.Join(outDir, fixturegen.ReceiptFixtureName), "receipt"); err != nil {
		return err
	}
	if err := fixturegen.WritePNG(filepath.Join(outDir, fixturegen.BlankPhotoFixtureName), "blank"); err != nil {
		return err
	}

	voicePath := filepath.Join(outDir, fixturegen.VoiceFixtureName)
	audioPath := filepath.Join(outDir, fixturegen.AudioFixtureName)
	switch {
	case hasMilenaVoice():
		tmpDir, err := os.MkdirTemp("", "tg-e2e-fixtures-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)

		voiceAIFF := filepath.Join(tmpDir, "e2e-voice.aiff")
		audioAIFF := filepath.Join(tmpDir, "e2e-audio.aiff")
		if err := runCmd("", "say", "-v", "Milena", "-o", voiceAIFF, "сметана завтра"); err != nil {
			return err
		}
		if err := runCmd("", "say", "-v", "Milena", "-o", audioAIFF, "йогурт послезавтра"); err != nil {
			return err
		}
		if err := runCmd("", "ffmpeg", "-y", "-i", voiceAIFF, "-c:a", "libopus", voicePath); err != nil {
			return err
		}
		if err := runCmd("", "ffmpeg", "-y", "-i", audioAIFF, "-c:a", "libmp3lame", audioPath); err != nil {
			return err
		}
	case fileExists(voicePath) && fileExists(audioPath):
		fmt.Fprintf(os.Stderr, "reusing existing speech fixtures in %s\n", outDir)
	case strings.TrimSpace(os.Getenv("TG_E2E_ALLOW_SYNTHETIC_AUDIO_FIXTURES")) == "1":
		if err := runCmd("", "ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=880:duration=1", "-c:a", "libopus", voicePath); err != nil {
			return err
		}
		if err := runCmd("", "ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=660:duration=1", "-c:a", "libmp3lame", audioPath); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "warning: generated synthetic non-speech audio fixtures because TG_E2E_ALLOW_SYNTHETIC_AUDIO_FIXTURES=1")
	default:
		return fmt.Errorf("speech fixtures require macOS say+Milena or preexisting %s/%s in %s", fixturegen.VoiceFixtureName, fixturegen.AudioFixtureName, outDir)
	}

	if err := os.WriteFile(filepath.Join(outDir, fixturegen.DocumentFixtureName), []byte("unsupported payload fixture\n"), 0o644); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "generated fixtures in %s\n", outDir)
	return nil
}

func hasMilenaVoice() bool {
	if _, err := exec.LookPath("say"); err != nil {
		return false
	}
	cmd := exec.Command("say", "-v", "?")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "Milena ") {
			return true
		}
	}
	return false
}

func runCmd(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
