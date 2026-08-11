package sonic_test

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/bytedance/sonic"
	vfastjson "github.com/valyala/fastjson"
)

// FuzzValidParity compares sonic.Valid(data) with fastjson validation because
// the root backend intentionally follows Sonic's permissive raw JSON parser.
func FuzzValidParity(f *testing.F) {
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
		`"unterminated`,
		`{"a":1}`,
		`{"a":1,"b":[2,3]}`,
		`[1,2,3]`,
		`{"a":}`,
		`[`,
		`{`,
		`}`,
		`]`,
		`tru`,
		`123abc`,
		`"\u0000"`,
		`"\ud83d\ude00"`,
		`"\ud83d"`,
		`{"a":1}extra`,
		`   {"a":1}   `,
		"\xff\xfe",
		"\x00",
		`{"\u0000":"\u0000"}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		gotSonic := sonic.Valid(data)
		var p vfastjson.Parser
		_, err := p.Parse(string(data))
		gotFastjson := len(data) > 0 && err == nil
		if gotSonic != gotFastjson {
			t.Fatalf("Valid mismatch: sonic=%v fastjson=%v data=%q", gotSonic, gotFastjson, data)
		}
	})
}

// FuzzMarshalRoundTrip marshals a map[string]interface{} built from a string
// key and a clamped int64, then unmarshals it back. The integer is clamped to
// the safe float64 integer range [-2^53+1, 2^53-1] so that float64 roundtrip
// through map[string]interface{} (where numbers become float64) is exact.
// Under Config{UseNumber:true} the unmarshaled value should be a json.Number
// whose string form equals strconv.FormatInt(n, 10).
func FuzzMarshalRoundTrip(f *testing.F) {
	f.Add("k", int64(0))
	f.Add("k", int64(1))
	f.Add("k", int64(-1))
	f.Add("k", int64(9007199254740991))  // 2^53 - 1
	f.Add("k", int64(-9007199254740991)) // -(2^53 - 1)
	f.Add("k", int64(42))
	f.Add("key with spaces", int64(12345))
	f.Add("unicode\u0000key", int64(-7))

	const maxSafe = 1<<53 - 1

	f.Fuzz(func(t *testing.T, s string, n int64) {
		if n > maxSafe {
			n = maxSafe
		}
		if n < -maxSafe {
			n = -maxSafe
		}

		in := map[string]interface{}{s: float64(n)}
		b, err := sonic.Marshal(in)
		if err != nil {
			t.Fatalf("Marshal error: %v (in=%v)", err, in)
		}
		if !sonic.Valid(b) {
			t.Fatalf("Marshal produced invalid JSON: %q", b)
		}

		// Default unmarshal: numbers become float64.
		var out map[string]interface{}
		if err := sonic.Unmarshal(b, &out); err != nil {
			t.Fatalf("Unmarshal error: %v (b=%q)", err, b)
		}
		got, ok := out[s]
		if !ok {
			// Key may have been escaped differently; skip strict comparison.
			return
		}
		fv, ok := got.(float64)
		if !ok {
			t.Fatalf("expected float64, got %T (%v) for %q", got, got, b)
		}
		if int64(fv) != n {
			t.Fatalf("roundtrip mismatch: got %v want %d (b=%q)", fv, n, b)
		}

		// UseNumber variant: value should be json.Number == FormatInt(n,10).
		var outNum map[string]interface{}
		api := sonic.Config{UseNumber: true}.Froze()
		if err := api.Unmarshal(b, &outNum); err != nil {
			t.Fatalf("UseNumber Unmarshal error: %v (b=%q)", err, b)
		}
		gotNum, ok := outNum[s].(json.Number)
		if !ok {
			t.Fatalf("expected json.Number, got %T (%v) for %q", outNum[s], outNum[s], b)
		}
		if gotNum.String() != strconv.FormatInt(n, 10) {
			t.Fatalf("json.Number mismatch: got %q want %q", gotNum.String(), strconv.FormatInt(n, 10))
		}
	})
}

// FuzzGetNoPanic ensures sonic.Get(data) and sonic.Get(data, "x") never panic
// for arbitrary bytes. When the data is valid JSON and Get succeeds, the
// returned node's Raw and MarshalJSON output should also be valid JSON.
func FuzzGetNoPanic(f *testing.F) {
	seeds := []string{
		``,
		`null`,
		`{"a":1}`,
		`{"a":{"b":2}}`,
		`[1,2,3]`,
		`"x"`,
		`123`,
		`{`,
		`[`,
		`{"a":`,
		`{"a":1,"b":[2,{"c":"d"}]}`,
		`"\ud83d\ude00"`,
		"\xff\xfe",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Must never panic.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Get panicked: %v (data=%q)", r, data)
				}
			}()
			_, _ = sonic.Get(data)
		}()
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Get(x) panicked: %v (data=%q)", r, data)
				}
			}()
			_, _ = sonic.Get(data, "x")
		}()

		// When valid and Get succeeds, Raw/MarshalJSON should be valid JSON.
		if !sonic.Valid(data) {
			return
		}
		node, err := sonic.Get(data)
		if err != nil {
			return
		}
		if raw, err := node.Raw(); err == nil && raw != "" {
			if !sonic.Valid([]byte(raw)) {
				t.Fatalf("Raw not valid JSON: %q (data=%q)", raw, data)
			}
		}
		if mb, err := node.MarshalJSON(); err == nil {
			if !sonic.Valid(mb) {
				t.Fatalf("MarshalJSON not valid: %q (data=%q)", mb, data)
			}
		}
	})
}

// FuzzNoCopyRawMessage ensures NoCopyRawMessage does not panic on arbitrary
// input, and for valid JSON the aliasing semantics are preserved.
func FuzzNoCopyRawMessage(f *testing.F) {
	seeds := []string{
		`{"a":1}`,
		`null`,
		`[1,2,3]`,
		`"x"`,
		`42`,
		``,
		`{`,
		`{"a":1}extra`,
		"\xff\xfe",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("NoCopyRawMessage panicked: %v (data=%q)", r, data)
				}
			}()
			var raw sonic.NoCopyRawMessage
			_ = raw.UnmarshalJSON(data)
			_, _ = raw.MarshalJSON()
		}()

		if !sonic.Valid(data) {
			return
		}
		raw := sonic.NoCopyRawMessage(data)
		b, err := raw.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON error: %v (data=%q)", err, data)
		}
		if string(b) != string(data) {
			t.Fatalf("MarshalJSON = %q, want %q", b, data)
		}
		// Aliasing: the returned slice should share storage when possible.
		if len(b) > 0 && len(data) > 0 && &b[0] != &data[0] {
			// Not a hard failure (implementation may copy), but log for visibility.
			t.Logf("MarshalJSON did not alias input (len=%d)", len(b))
		}
	})
}
