package unquote

import (
	"testing"

	nativetypes "github.com/bytedance/sonic/internal/native/types"
)

func TestStringAndIntoBytes(t *testing.T) {
	s, err := String(`a\nb`)
	if err != 0 || s != "a\nb" {
		t.Fatalf("String = %q, %v", s, err)
	}
	var dst []byte
	if err := IntoBytes(`x`, &dst); err != 0 {
		t.Fatalf("IntoBytes error = %v", err)
	}
	if string(dst) != "x" {
		t.Fatalf("IntoBytes dst = %q", dst)
	}
	if _, err := String(`\uZZZZ`); err != nativetypes.ERR_INVALID_ESCAPE && err != nativetypes.ERR_INVALID_UNICODE {
		t.Fatalf("invalid escape error = %v", err)
	}
	if _, err := String("bad\ncontrol"); err != nativetypes.ERR_INVALID_CHAR {
		t.Fatalf("raw control error = %v, want ERR_INVALID_CHAR", err)
	}
}

func TestEscapeSequences(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`\"`, "\""},
		{`\\`, "\\"},
		{`\/`, "/"},
		{`\b`, "\b"},
		{`\f`, "\f"},
		{`\n`, "\n"},
		{`\r`, "\r"},
		{`\t`, "\t"},
		{`a\tb\n\\c\"d`, "a\tb\n\\c\"d"},
	}
	for _, c := range cases {
		got, err := String(c.in)
		if err != 0 {
			t.Errorf("String(%q) error = %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("String(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUnicodeEscape(t *testing.T) {
	// Basic BMP code point.
	s, err := String(`\u0041`)
	if err != 0 || s != "A" {
		t.Fatalf("\\u0041 = %q, %v", s, err)
	}
	// Multibyte BMP code point.
	s, err = String(`\u4e16`)
	if err != 0 || s != "世" {
		t.Fatalf("\\u4e16 = %q, %v", s, err)
	}
}

func TestSurrogatePair(t *testing.T) {
	// U+1F600 GRINNING FACE expressed as a UTF-16 surrogate pair.
	s, err := String(`\uD83D\uDE00`)
	if err != 0 {
		t.Fatalf("surrogate pair error = %v", err)
	}
	if s != "😀" {
		t.Fatalf("surrogate pair = %q, want 😀", s)
	}
	if []byte(s) == nil {
		t.Fatalf("surrogate pair returned nil bytes")
	}
}

func TestHighSurrogateWithoutPair(t *testing.T) {
	// Lone high surrogate at end of input.
	if _, err := String(`\uD83D`); err != nativetypes.ERR_INVALID_UNICODE {
		t.Fatalf("lone high surrogate error = %v, want ERR_INVALID_UNICODE", err)
	}
	// High surrogate followed by non-surrogate escape.
	if _, err := String(`\uD83D\u0041`); err != nativetypes.ERR_INVALID_UNICODE {
		t.Fatalf("high surrogate + non-surrogate error = %v, want ERR_INVALID_UNICODE", err)
	}
	// High surrogate followed by a plain letter (no \uXXXX).
	if _, err := String(`\uD83Dabc`); err != nativetypes.ERR_INVALID_UNICODE {
		t.Fatalf("high surrogate + plain error = %v, want ERR_INVALID_UNICODE", err)
	}
}

func TestLowSurrogateAlone(t *testing.T) {
	if _, err := String(`\uDE00`); err != nativetypes.ERR_INVALID_UNICODE {
		t.Fatalf("lone low surrogate error = %v, want ERR_INVALID_UNICODE", err)
	}
}

func TestTruncatedUnicodeEscape(t *testing.T) {
	// Less than 4 hex digits after \u.
	if _, err := String(`\u00`); err != nativetypes.ERR_INVALID_ESCAPE {
		t.Fatalf("truncated \\u error = %v, want ERR_INVALID_ESCAPE", err)
	}
	// Backslash-u at end of input.
	if _, err := String(`\u`); err != nativetypes.ERR_INVALID_ESCAPE {
		t.Fatalf("truncated \\u error = %v, want ERR_INVALID_ESCAPE", err)
	}
}

func TestUnknownEscape(t *testing.T) {
	if _, err := String(`\x`); err != nativetypes.ERR_INVALID_ESCAPE {
		t.Fatalf("unknown escape error = %v, want ERR_INVALID_ESCAPE", err)
	}
	// Lone trailing backslash.
	if _, err := String(`a\`); err != nativetypes.ERR_INVALID_ESCAPE {
		t.Fatalf("trailing backslash error = %v, want ERR_INVALID_ESCAPE", err)
	}
}

func TestRawControlChars(t *testing.T) {
	for _, c := range []byte{0, 1, 0x1F} {
		if _, err := String("a" + string(c) + "b"); err != nativetypes.ERR_INVALID_CHAR {
			t.Fatalf("raw control 0x%02x error = %v, want ERR_INVALID_CHAR", c, err)
		}
	}
	// Tab (0x09) is technically a control char (< 0x20) per the contract
	// and should be rejected when not escaped.
	if _, err := String("a\tb"); err != nativetypes.ERR_INVALID_CHAR {
		t.Fatalf("raw tab error = %v, want ERR_INVALID_CHAR", err)
	}
}

func TestInvalidUTF8(t *testing.T) {
	// A lone continuation byte is invalid UTF-8.
	if _, err := String("a\xc0b"); err != nativetypes.ERR_INVALID_UTF8 {
		t.Fatalf("invalid utf8 error = %v, want ERR_INVALID_UTF8", err)
	}
	// A continuation byte without a leader.
	if _, err := String("\x80"); err != nativetypes.ERR_INVALID_UTF8 {
		t.Fatalf("lone continuation error = %v, want ERR_INVALID_UTF8", err)
	}
}

func TestIntoBytesResetsExistingSlice(t *testing.T) {
	dst := []byte("preexisting")
	if err := IntoBytes(`x`, &dst); err != 0 {
		t.Fatalf("IntoBytes error = %v", err)
	}
	if string(dst) != "x" {
		t.Fatalf("IntoBytes dst = %q, want \"x\"", dst)
	}
	// Capacity should be preserved (no reallocation expected for short input).
	if cap(dst) < len("preexisting") {
		t.Fatalf("IntoBytes reallocated dst, cap = %d", cap(dst))
	}
}

func TestIntoBytesNilDestination(t *testing.T) {
	if err := IntoBytes(`x`, nil); err != nativetypes.ERR_UNSUPPORT_TYPE {
		t.Fatalf("nil destination error = %v, want ERR_UNSUPPORT_TYPE", err)
	}
}

func TestValidUTF8PassesThrough(t *testing.T) {
	s, err := String("héllo 世界")
	if err != 0 || s != "héllo 世界" {
		t.Fatalf("passthrough UTF-8 = %q, %v", s, err)
	}
}

func TestEmptyString(t *testing.T) {
	s, err := String("")
	if err != 0 || s != "" {
		t.Fatalf("empty String = %q, %v", s, err)
	}
	var dst []byte
	if err := IntoBytes("", &dst); err != 0 {
		t.Fatalf("empty IntoBytes error = %v", err)
	}
	if len(dst) != 0 {
		t.Fatalf("empty IntoBytes dst = %q", dst)
	}
}
