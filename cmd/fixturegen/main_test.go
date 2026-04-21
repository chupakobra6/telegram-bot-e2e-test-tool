package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestRunWritesFixturePNG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.png")

	if err := run([]string{"--output", path}); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("os.Open() error = %v", err)
	}
	defer file.Close()

	img, err := png.Decode(file)
	if err != nil {
		t.Fatalf("png.Decode() error = %v", err)
	}
	if img.Bounds().Dx() != 1280 || img.Bounds().Dy() != 720 {
		t.Fatalf("unexpected bounds: %v", img.Bounds())
	}
}
