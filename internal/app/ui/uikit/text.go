// Package uikit holds the shared building blocks of the Meridian interface:
// monospace runs, section labels, truncation and empty states, so panels
// present the same things the same way.
package uikit

const ellipsis = "…"

// MiddleTruncate shortens s to fit width by replacing its middle with an
// ellipsis, keeping the start and end (a type path's root and leaf). measure
// returns a string's width.
func MiddleTruncate(s string, width float32, measure func(string) float32) string {
	if measure(s) <= width {
		return s
	}
	runes := []rune(s)
	keep := len(runes)
	for keep > 0 {
		keep--
		head := (keep + 1) / 2
		tail := keep - head
		candidate := string(runes[:head]) + ellipsis + string(runes[len(runes)-tail:])
		if measure(candidate) <= width {
			return candidate
		}
	}
	if measure(ellipsis) <= width {
		return ellipsis
	}
	return ""
}

// EndTruncate shortens s to fit width with a trailing ellipsis.
func EndTruncate(s string, width float32, measure func(string) float32) string {
	if measure(s) <= width {
		return s
	}
	runes := []rune(s)
	for n := len(runes) - 1; n > 0; n-- {
		if candidate := string(runes[:n]) + ellipsis; measure(candidate) <= width {
			return candidate
		}
	}
	return ""
}
