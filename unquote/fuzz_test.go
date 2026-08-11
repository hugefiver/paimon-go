package unquote

import (
	"testing"
	"unicode"
	"unicode/utf8"

	nativetypes "github.com/bytedance/sonic/internal/native/types"
)

// FuzzStringNoPanic ensures unquote.String never panics on arbitrary input,
// and that for inputs without backslash, control characters, or invalid UTF-8,
// the output equals the input (identity).
func FuzzStringNoPanic(f *testing.F) {
	seeds := []string{
		``,
		`hello`,
		`hello world`,
		`a\/b`,
		`\n`,
		`\t`,
		`emoji✓`,
		"\xff\xfe",
		"\x00",
		`backslash\\here`,
		`tab\there`,
		`unicode\u0000`,
		"plain text",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		var out string
		var e nativetypes.ParsingError
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("String panicked: %v (s=%q)", r, s)
				}
			}()
			out, e = String(s)
		}()

		// If the input is plain (no backslash, no control chars, valid UTF-8)
		// and the function succeeded, the output must equal the input.
		if e == 0 && isPlainASCIIish(s) && utf8.ValidString(s) {
			if out != s {
				t.Fatalf("identity mismatch: got %q want %q", out, s)
			}
		}
	})
}

// FuzzIntoBytesNoPanic ensures IntoBytes never panics on arbitrary input.
func FuzzIntoBytesNoPanic(f *testing.F) {
	seeds := []string{
		``,
		`hello`,
		`a\/b`,
		`\n`,
		"\xff\xfe",
		"\x00",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("IntoBytes panicked: %v (s=%q)", r, s)
				}
			}()
			var dst []byte
			_ = IntoBytes(s, &dst)
		}()
	})
}

// isPlainASCIIish returns true if s contains no backslash and no control
// characters. It does not require valid UTF-8 (that check is separate).
func isPlainASCIIish(s string) bool {
	for _, r := range s {
		if r == '\\' {
			return false
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return false
		}
		if r == unicode.ReplacementChar {
			return false
		}
	}
	return true
}
