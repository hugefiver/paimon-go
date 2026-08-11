package utf8

import (
	"testing"
	"unicode/utf8"
)

// FuzzValidateNoPanic ensures Validate and ValidateString never panic on
// arbitrary bytes, and agree with unicode/utf8.Valid.
func FuzzValidateNoPanic(f *testing.F) {
	seeds := [][]byte{
		{},
		[]byte("hello"),
		[]byte("unicode✓"),
		{0xff, 0xfe},
		{0x00},
		[]byte("a"),
		{0xc0, 0x80},
		{0xe0, 0xa0, 0x80},
		{0xf0, 0x9f, 0x98, 0x80},
		{0xff},
		{0xfe},
		{0xef, 0xbf, 0xbf}, // U+FFFF
		nil,
	}
	for _, b := range seeds {
		f.Add([]byte(b))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var gotByte bool
		var gotStr bool
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Validate panicked: %v (data=%v)", r, data)
				}
			}()
			gotByte = Validate(data)
			gotStr = ValidateString(string(data))
		}()

		want := utf8.Valid(data)
		if gotByte != want {
			t.Fatalf("Validate mismatch: got %v want %v (data=%v)", gotByte, want, data)
		}
		if gotStr != want {
			t.Fatalf("ValidateString mismatch: got %v want %v (data=%v)", gotStr, want, data)
		}
	})
}

// FuzzCorrectWithNoPanic ensures CorrectWith never panics. The corrected output
// is valid UTF-8 when the source is already valid or the caller supplies a valid
// replacement string; Sonic appends the replacement verbatim, so invalid
// replacement strings can produce invalid output.
func FuzzCorrectWithNoPanic(f *testing.F) {
	seeds := []struct {
		src  []byte
		repl string
	}{
		{[]byte("hello"), "?"},
		{[]byte{0xff, 0xfe}, "?"},
		{[]byte{0x00}, "?"},
		{[]byte("unicode✓"), "?"},
		{[]byte{0xc0, 0x80}, "?"},
		{[]byte{0xf0, 0x9f, 0x98, 0x80}, "?"},
		{nil, "?"},
		{[]byte("mix\xffvalid"), ""},
		{[]byte("mix\xffvalid"), "INVALID"},
	}
	for _, s := range seeds {
		f.Add([]byte(s.src), s.repl)
	}

	f.Fuzz(func(t *testing.T, src []byte, repl string) {
		var got []byte
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("CorrectWith panicked: %v (src=%v repl=%q)", r, src, repl)
				}
			}()
			got = CorrectWith(nil, src, repl)
		}()

		if (utf8.Valid(src) || utf8.ValidString(repl)) && !utf8.Valid(got) {
			t.Fatalf("CorrectWith produced invalid UTF-8: %q (src=%v repl=%q)", got, src, repl)
		}
		// If src was already valid UTF-8 and repl is empty, output should equal src.
		if utf8.Valid(src) && repl == "" {
			if string(got) != string(src) {
				t.Fatalf("CorrectWith altered valid UTF-8: got %q want %q", got, src)
			}
		}
	})
}
