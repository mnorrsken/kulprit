package render

import (
	"fmt"
	"strings"
)

const (
	reset = "\x1b[0m"
	dim   = "\x1b[2m"
)

// palette holds xterm-256 background colours that are light enough to carry
// black text, so a line stays readable whether the terminal is light or dark.
var palette = []int{117, 150, 223, 183, 158, 216, 189, 194, 210, 180, 153, 222}

// sharedColor marks a field owned by more than one manager.
const sharedColor = 253

func bgCode(n int) string { return fmt.Sprintf("\x1b[48;5;%d;38;5;16m", n) }

// managerColor is the background for manager index i, cycling through the
// palette when an object has more managers than there are colours.
func managerColor(i int) string { return bgCode(palette[i%len(palette)]) }

// paint wraps the text of a line, leaving its indentation bare so the shape of
// the document stays visible.
func paint(text, code string) string {
	if code == "" || strings.TrimSpace(text) == "" {
		return text
	}
	i := len(text) - len(strings.TrimLeft(text, " "))
	return text[:i] + code + text[i:] + reset
}

// swatch is a solid block of a colour, for the legend.
func swatch(code string) string {
	if code == "" {
		return "  "
	}
	return code + "  " + reset
}
