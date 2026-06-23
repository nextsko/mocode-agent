// Package screencap provides screenshot capture tool for oMo code.
package screencap

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/kbinani/screenshot"
)

// CapturePNG captures a screenshot and saves as PNG.
// Returns the file path.
func CapturePNG(outputDir string) (string, error) {
	count := screenshot.NumActiveDisplays()
	if count <= 0 {
		return "", fmt.Errorf("no active displays found")
	}

	var img image.Image
	var err error

	// Capture all displays into a single image.
	bounds := screenshot.GetDisplayBounds(0)
	for i := 1; i < count; i++ {
		bounds = bounds.Union(screenshot.GetDisplayBounds(i))
	}
	canvas := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for i := 0; i < count; i++ {
		displayBounds := screenshot.GetDisplayBounds(i)
		img, err = screenshot.CaptureRect(displayBounds)
		if err != nil {
			return "", fmt.Errorf("capture display %d: %w", i, err)
		}
		draw.Draw(canvas, displayBounds.Sub(bounds.Min), img, img.Bounds().Min, draw.Src)
	}

	if outputDir == "" {
		outputDir = os.TempDir()
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return "", err
	}

	filename := "screenshot-" + time.Now().Format("20060102-150405") + ".png"
	path := filepath.Join(outputDir, filename)

	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if err := png.Encode(file, canvas); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
