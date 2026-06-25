package wechat

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"

	"charm.land/lipgloss/v2"
	qrcode "github.com/yeqown/go-qrcode"
)

// QRGenerator generates QR codes as ASCII art.
type QRGenerator struct{}

// NewQRGenerator creates a new QR generator.
func NewQRGenerator() *QRGenerator {
	return &QRGenerator{}
}

// Generate creates a QR code from a URL string and returns its ASCII art.
func (g *QRGenerator) Generate(url string) (string, error) {
	result, err := g.GenerateResult(url)
	if err != nil {
		return "", err
	}
	return result.ASCII, nil
}

func (g *QRGenerator) GenerateResult(url string) (*QRResult, error) {
	const internalModuleSize = 10

	qrc, err := qrcode.New(url,
		qrcode.WithQRWidth(internalModuleSize),
		qrcode.WithBuiltinImageEncoder(qrcode.PNG_FORMAT),
	)
	if err != nil {
		return nil, fmt.Errorf("create qr: %w", err)
	}

	attr, err := qrc.Attribute()
	if err != nil {
		return nil, fmt.Errorf("get attributes: %w", err)
	}

	var buf bytes.Buffer
	if err := qrc.SaveTo(&buf); err != nil {
		return nil, fmt.Errorf("render png: %w", err)
	}

	img, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("decode png: %w", err)
	}

	return &QRResult{
		Content:    url,
		ASCII:      imageToASCII(img, attr),
		PNGDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
	}, nil
}

func imageToASCII(img image.Image, attr *qrcode.Attribute) string {
	moduleSize := attr.BlockWidth
	borderLeft := attr.Borders[3]
	borderTop := attr.Borders[0]

	moduleCols := (attr.W - attr.Borders[1] - attr.Borders[3]) / moduleSize
	moduleRows := (attr.H - attr.Borders[0] - attr.Borders[2]) / moduleSize

	var result bytes.Buffer
	for y := 0; y < moduleRows; y += 2 {
		for x := 0; x < moduleCols; x++ {
			top := isModuleDark(img, x, y, moduleSize, borderLeft, borderTop)
			bottom := false
			if y+1 < moduleRows {
				bottom = isModuleDark(img, x, y+1, moduleSize, borderLeft, borderTop)
			}

			switch {
			case top && bottom:
				result.WriteString("█")
			case top:
				result.WriteString("▀")
			case bottom:
				result.WriteString("▄")
			default:
				result.WriteString(" ")
			}
		}
		if y < moduleRows-2 {
			result.WriteString("\n")
		}
	}
	return result.String()
}

func isModuleDark(img image.Image, moduleX, moduleY, moduleSize, borderLeft, borderTop int) bool {
	px := borderLeft + moduleX*moduleSize + moduleSize/2
	py := borderTop + moduleY*moduleSize + moduleSize/2

	bounds := img.Bounds()
	if px >= bounds.Max.X {
		px = bounds.Max.X - 1
	}
	if py >= bounds.Max.Y {
		py = bounds.Max.Y - 1
	}

	r, gv, b, _ := img.At(px, py).RGBA()
	r >>= 8
	gv >>= 8
	b >>= 8
	luminance := (19595*r + 38470*gv + 7472*b + 1<<15) >> 16
	return luminance < 128
}

// GenerateQR is a convenience function that creates a QR code from a URL.
func GenerateQR(content string) (*QRResult, error) {
	g := NewQRGenerator()
	return g.GenerateResult(content)
}

// QRResult holds the QR art and raw data.
type QRResult struct {
	Content    string
	ASCII      string
	PNGDataURL string
}

// RenderQRPanel renders a pre-generated QR ASCII string as a styled terminal panel.
func RenderQRPanel(title, status, qrASCII string) string {
	qrStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#FFFFFF")).
		Foreground(lipgloss.Color("#000000")).
		Padding(1, 2)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#07C160"))

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888"))

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#07C160")).
		Padding(1, 2)

	qrContent := qrStyle.Render(qrASCII)
	panel := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(title),
		"",
		qrContent,
		"",
		statusStyle.Render("Status: "+status),
		statusStyle.Render("Please use WeChat to scan the QR code"),
	)

	return borderStyle.Render(panel)
}
