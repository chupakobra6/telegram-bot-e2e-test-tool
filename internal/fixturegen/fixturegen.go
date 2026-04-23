package fixturegen

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	PhotoFixtureName      = "e2e-photo.png"
	ReceiptFixtureName    = "e2e-receipt.png"
	BlankPhotoFixtureName = "e2e-blank-photo.png"
	VoiceFixtureName      = "e2e-voice.ogg"
	AudioFixtureName      = "e2e-audio.mp3"
	DocumentFixtureName   = "e2e-document.txt"
)

func WritePNG(outputPath, preset string) error {
	if outputPath == "" {
		return fmt.Errorf("output path is required")
	}

	img, err := BuildFixture(preset)
	if err != nil {
		return err
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		return fmt.Errorf("encode png: %w", err)
	}
	return nil
}

func BuildFixture(preset string) (*image.RGBA, error) {
	switch preset {
	case "package":
		return buildTextFixture([]string{
			"сметана",
			"завтра",
		}, false)
	case "receipt":
		return buildTextFixture([]string{
			"КАССОВЫЙ ЧЕК",
			"",
			"кефир",
			"годен до 22.04.2026",
			"итог 120.00",
		}, true)
	case "blank":
		return buildBlankFixture(), nil
	default:
		return nil, fmt.Errorf("unsupported --preset %q", preset)
	}
}

func buildBlankFixture() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{250, 248, 242, 255}}, image.Point{}, draw.Src)
	drawRect(img, image.Rect(40, 40, 1240, 680), color.RGBA{24, 28, 36, 255})
	drawRect(img, image.Rect(60, 60, 1220, 660), color.RGBA{255, 255, 255, 255})
	drawRect(img, image.Rect(90, 100, 520, 180), color.RGBA{19, 106, 154, 255})
	drawRect(img, image.Rect(90, 210, 1190, 250), color.RGBA{40, 40, 40, 255})
	drawRect(img, image.Rect(90, 270, 980, 305), color.RGBA{65, 65, 65, 255})
	drawRect(img, image.Rect(90, 355, 860, 395), color.RGBA{25, 120, 82, 255})
	drawRect(img, image.Rect(90, 420, 1110, 455), color.RGBA{65, 65, 65, 255})
	drawRect(img, image.Rect(90, 520, 420, 610), color.RGBA{232, 240, 245, 255})
	drawRect(img, image.Rect(470, 520, 870, 610), color.RGBA{245, 238, 230, 255})
	drawRect(img, image.Rect(920, 520, 1120, 610), color.RGBA{230, 245, 233, 255})
	return img
}

func buildTextFixture(lines []string, receipt bool) (*image.RGBA, error) {
	const width = 1280
	const height = 720

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{242, 236, 224, 255}}, image.Point{}, draw.Src)

	cardRect := image.Rect(90, 40, 1190, 680)
	cardColor := color.RGBA{255, 255, 255, 255}
	if receipt {
		cardRect = image.Rect(240, 20, 1040, 700)
		cardColor = color.RGBA{252, 252, 252, 255}
	}
	drawRect(img, cardRect, cardColor)

	ttf, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, fmt.Errorf("parse embedded font: %w", err)
	}
	size := 64.0
	if receipt {
		size = 54.0
	}
	face, err := opentype.NewFace(ttf, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("create embedded font face: %w", err)
	}
	defer face.Close()

	d := &font.Drawer{
		Dst:  img,
		Src:  image.Black,
		Face: face,
		Dot:  fixed.P(cardRect.Min.X+60, cardRect.Min.Y+110),
	}
	lineAdvance := int(size) + 28
	if receipt {
		lineAdvance = int(size) + 22
	}
	for _, line := range lines {
		if line == "" {
			d.Dot.Y += fixed.I(lineAdvance)
			continue
		}
		d.DrawString(line)
		d.Dot.X = fixed.I(cardRect.Min.X + 60)
		d.Dot.Y += fixed.I(lineAdvance)
	}
	return img, nil
}

func drawRect(img *image.RGBA, rect image.Rectangle, fill color.RGBA) {
	draw.Draw(img, rect, &image.Uniform{C: fill}, image.Point{}, draw.Src)
}
