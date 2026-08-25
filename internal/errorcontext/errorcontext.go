// Package errorcontext formats source excerpts for public decoder errors.
package errorcontext

import (
	"fmt"
	"strings"
)

// SourceDescription returns the native-style diagnostic for message at pos in
// src. Valid positions show up to 32 bytes centered on the failing byte;
// invalid positions show the full source with the caret at the start.
func SourceDescription(src string, pos int, message string) string {
	lbound, lwidth, rbound, rwidth := sourceBounds(len(src), pos)
	return fmt.Sprintf(
		"at index %d: %s\n\n\t%s\n\t%s^%s\n",
		pos,
		message,
		src[lbound:rbound],
		strings.Repeat(".", lwidth),
		strings.Repeat(".", rwidth),
	)
}

func sourceBounds(size, pos int) (lbound, lwidth, rbound, rwidth int) {
	if pos >= size || pos < 0 {
		return 0, 0, size, 0
	}
	i := 16
	lbound = pos - i
	rbound = pos + i
	if lbound < 0 {
		lbound, rbound, i = 0, rbound-lbound, i+lbound
	}
	if n := size; rbound > n {
		n = rbound - n
		rbound = size
		if lbound > n {
			i += n
			lbound -= n
		}
	}
	lwidth = clampZero(i)
	rwidth = clampZero(rbound - lbound - i - 1)
	return
}

func clampZero(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
