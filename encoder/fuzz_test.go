package encoder

import (
	"encoding/json"
	"strconv"
	"testing"
)

// FuzzQuote compares encoder.Quote with strconv.Quote for arbitrary strings.
func FuzzQuote(f *testing.F) {
	seeds := []string{
		``,
		`hello`,
		`hello\nworld`,
		`"quoted"`,
		`tab\there`,
		`unicode✓`,
		"\x00",
		"\xff\xfe",
		"backslash\\here",
		`newline
here`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		got := Quote(s)
		want := strconv.Quote(s)
		if got != want {
			t.Fatalf("Quote mismatch: got %q want %q (input=%q)", got, want, s)
		}
	})
}

// FuzzValid compares encoder.Valid with encoding/json.Valid for arbitrary
// bytes. encoder.Valid returns (ok, start) and wraps json.Valid today.
func FuzzValid(f *testing.F) {
	seeds := []string{
		``,
		`null`,
		`{"a":1}`,
		`[1,2,3]`,
		`"x"`,
		`123`,
		`{`,
		`[`,
		`{"a":1}extra`,
		`tru`,
		"\xff\xfe",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		gotOk, _ := Valid(data)
		wantOk := json.Valid(data)
		if gotOk != wantOk {
			t.Fatalf("Valid mismatch: got %v want %v (data=%q)", gotOk, wantOk, data)
		}
	})
}

// FuzzEncodeNoPanic ensures Encode never panics on arbitrary values/opts.
func FuzzEncodeNoPanic(f *testing.F) {
	seeds := []struct {
		val  string
		opts uint64
	}{
		{`null`, 0},
		{`{"a":1}`, uint64(SortMapKeys)},
		{`[1,2,3]`, uint64(EscapeHTML)},
		{`"x"`, 0},
		{`42`, 0},
	}
	for _, s := range seeds {
		f.Add(s.val, s.opts)
	}

	f.Fuzz(func(t *testing.T, src string, opts uint64) {
		var val interface{}
		if err := json.Unmarshal([]byte(src), &val); err != nil {
			t.Skip()
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Encode panicked: %v (src=%q opts=%d)", r, src, opts)
				}
			}()
			_, _ = Encode(val, Options(opts))
		}()
	})
}

// FuzzEncodeRoundTrip ensures that for valid JSON inputs Encode produces valid
// JSON output (not necessarily identical).
func FuzzEncodeRoundTrip(f *testing.F) {
	seeds := []string{
		`null`,
		`true`,
		`false`,
		`0`,
		`1.5`,
		`"hello"`,
		`{"a":1}`,
		`[1,2,3]`,
		`{"a":{"b":[1,2]}}`,
		`{"html":"<tag>"}`,
		`{"key":"val"}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, src string) {
		if !json.Valid([]byte(src)) {
			t.Skip()
		}
		var val interface{}
		if err := json.Unmarshal([]byte(src), &val); err != nil {
			t.Skip()
		}
		b, err := Encode(val, 0)
		if err != nil {
			t.Fatalf("Encode error: %v (src=%q)", err, src)
		}
		if !json.Valid(b) {
			t.Fatalf("Encode produced invalid JSON: %q (src=%q)", b, src)
		}
	})
}
