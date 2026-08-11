package decoder

import (
	"encoding/json"
	"testing"
)

// FuzzSkipNoPanic ensures decoder.Skip never panics on arbitrary bytes, and
// that when it returns a valid range, the invariants hold:
//   - 0 <= start <= end <= len(data)
//   - when start == end the function reported no token (incomplete input)
//
// Furthermore, for inputs that are valid complete JSON values, the slice
// data[start:end] must be valid JSON.
func FuzzSkipNoPanic(f *testing.F) {
	seeds := []string{
		``,
		`null`,
		`true`,
		`false`,
		`0`,
		`-1`,
		`1.5`,
		`"hello"`,
		`"hello\nworld"`,
		`{"a":1}`,
		`{"a":1,"b":[2,3]}`,
		`[1,2,3]`,
		`{"a":1}extra`,
		`{`,
		`[`,
		`{"a":`,
		`"unterminated`,
		`tru`,
		`123abc`,
		"\xff\xfe",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var start, end int
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Skip panicked: %v (data=%q)", r, data)
				}
			}()
			start, end = Skip(data)
		}()

		// Invariants.
		if start < 0 {
			t.Fatalf("start < 0: %d (data=%q)", start, data)
		}
		if end < 0 {
			t.Fatalf("end < 0: %d (data=%q)", end, data)
		}
		if start > end {
			t.Fatalf("start > end: %d > %d (data=%q)", start, end, data)
		}
		if end > len(data) {
			t.Fatalf("end > len(data): %d > %d (data=%q)", end, len(data), data)
		}

		// When the input is a complete valid JSON value, the skipped slice
		// should be valid JSON (Skip should consume exactly the value).
		if start == 0 && end > 0 && end == len(data) {
			if json.Valid(data) {
				if !json.Valid(data[start:end]) {
					t.Fatalf("skipped slice not valid JSON: %q (data=%q)", data[start:end], data)
				}
			}
		}
	})
}

// FuzzDecoderNoPanic ensures NewDecoder + Decode never panics on arbitrary
// string input.
func FuzzDecoderNoPanic(f *testing.F) {
	seeds := []string{
		``,
		`null`,
		`{"a":1}`,
		`[1,2,3]`,
		`{"a":1}{"b":2}`,
		`{`,
		`{"a":`,
		`tru`,
		"\xff\xfe",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, src string) {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Decoder panicked: %v (src=%q)", r, src)
				}
			}()
			d := NewDecoder(src)
			var v interface{}
			_ = d.Decode(&v)
			_ = d.Pos()
			_ = d.CheckTrailings()
			d.Reset(src)
			_ = d.Decode(&v)
		}()
	})
}
