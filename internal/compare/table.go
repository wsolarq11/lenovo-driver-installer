package compare

import "strings"

// GetDisplayWidth mirrors Get-DisplayWidth with wide rune ranges.
func GetDisplayWidth(text string) int {
	width := 0
	for _, r := range text {
		if isWideRune(r) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func isWideRune(code rune) bool {
	return (code >= 0x1100 && code <= 0x115F) ||
		(code >= 0x2E80 && code <= 0x303E) ||
		(code >= 0x3041 && code <= 0x33FF) ||
		(code >= 0x3400 && code <= 0x4DBF) ||
		(code >= 0x4E00 && code <= 0x9FFF) ||
		(code >= 0xA000 && code <= 0xA4CF) ||
		(code >= 0xAC00 && code <= 0xD7A3) ||
		(code >= 0xF900 && code <= 0xFAFF) ||
		(code >= 0xFE30 && code <= 0xFE4F) ||
		(code >= 0xFF00 && code <= 0xFF60) ||
		(code >= 0xFFE0 && code <= 0xFFE6)
}

// GetTruncatedText mirrors Get-TruncatedText.
func GetTruncatedText(text string, maxWidth int) string {
	if text == "" || GetDisplayWidth(text) <= maxWidth {
		return text
	}
	var b strings.Builder
	used := 0
	for _, r := range text {
		charWidth := GetDisplayWidth(string(r))
		if used+charWidth+3 > maxWidth {
			return b.String() + "..."
		}
		b.WriteRune(r)
		used += charWidth
	}
	return b.String() + "..."
}

// FormatCell mirrors Format-Cell.
func FormatCell(text string, width int, right bool) string {
	pad := width - GetDisplayWidth(text)
	if pad < 0 {
		pad = 0
	}
	if right {
		return strings.Repeat(" ", pad) + text
	}
	return text + strings.Repeat(" ", pad)
}
