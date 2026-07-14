package tui

import (
	"strings"

	"github.com/skip2/go-qrcode"
)

// RenderQR returns a compact terminal QR code for content (URL or conf).
func RenderQR(content string) (string, error) {
	q, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", err
	}
	s := q.ToSmallString(false)
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s, nil
}
