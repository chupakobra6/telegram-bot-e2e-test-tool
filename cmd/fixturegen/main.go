package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *output == "" {
		return fmt.Errorf("--output is required")
	}

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

	file, err := os.Create(*output)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		return fmt.Errorf("encode png: %w", err)
	}
	return nil
}

func drawRect(img *image.RGBA, rect image.Rectangle, fill color.RGBA) {
	draw.Draw(img, rect, &image.Uniform{C: fill}, image.Point{}, draw.Src)
}
