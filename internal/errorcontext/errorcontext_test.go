package errorcontext

import (
	"strings"
	"testing"
)

func TestSourceDescription(t *testing.T) {
	const message = "bad"
	tests := []struct {
		name string
		src  string
		pos  int
		want string
	}{
		{
			name: "middle",
			src:  "xx?yy",
			pos:  2,
			want: "at index 2: bad\n\n\txx?yy\n\t..^..\n",
		},
		{
			name: "negative position",
			src:  "abc",
			pos:  -1,
			want: "at index -1: bad\n\n\tabc\n\t^\n",
		},
		{
			name: "position beyond source",
			src:  "abc",
			pos:  3,
			want: "at index 3: bad\n\n\tabc\n\t^\n",
		},
		{
			name: "40 byte source is bounded",
			src:  strings.Repeat("0123456789", 4),
			pos:  20,
			want: "at index 20: bad\n\n\t45678901234567890123456789012345\n\t................^...............\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SourceDescription(tt.src, tt.pos, message); got != tt.want {
				t.Fatalf("SourceDescription(%q, %d, %q) = %q, want %q", tt.src, tt.pos, message, got, tt.want)
			}
		})
	}
}
