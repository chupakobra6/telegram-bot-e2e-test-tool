package fixturegen

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestWritePNGWritesFixture(t *testing.T) {
	t.Parallel()

	for _, preset := range []string{"package", "receipt", "blank"} {
		preset := preset
		t.Run(preset, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "fixture.png")

			if err := WritePNG(path, preset); err != nil {
				t.Fatalf("WritePNG() error = %v", err)
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
		})
	}
}

func TestBuildFixtureRejectsUnknownPreset(t *testing.T) {
	t.Parallel()

	if _, err := BuildFixture("nope"); err == nil {
		t.Fatal("expected error for unknown preset")
	}
}
