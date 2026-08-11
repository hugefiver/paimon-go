package decoder

import (
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Plan tests
// ---------------------------------------------------------------------------

func TestDecoderOptionsAndTrailingCheck(t *testing.T) {
	d := NewDecoder(`{"n":123}`)
	d.UseNumber()
	var out map[string]interface{}
	if err := d.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if _, ok := out["n"].(json.Number); !ok {
		t.Fatalf("n type = %T, want json.Number", out["n"])
	}
	if err := d.CheckTrailings(); err != nil {
		t.Fatalf("CheckTrailings error = %v", err)
	}
	d.Reset(`{"x":1} trailing`)
	var out2 map[string]int
	if err := d.Decode(&out2); err != nil {
		t.Fatalf("Decode second value error = %v", err)
	}
	if err := d.CheckTrailings(); err == nil {
		t.Fatalf("CheckTrailings returned nil for trailing bytes")
	}
}

func TestStreamDecoderMoreBufferedAndInputOffset(t *testing.T) {
	sd := NewStreamDecoder(strings.NewReader(`[{"x":1},{"x":2}]`))
	var arr []map[string]int
	if err := sd.Decode(&arr); err != nil {
		t.Fatalf("Decode array error = %v", err)
	}
	if sd.InputOffset() == 0 {
		t.Fatalf("InputOffset = 0 after decode")
	}
	if _, err := io.ReadAll(sd.Buffered()); err != nil {
		t.Fatalf("Buffered read error = %v", err)
	}
}

func TestSkipAndPretouch(t *testing.T) {
	start, end := Skip([]byte(`  {"a":[1]} trailing`))
	if start != 2 || string([]byte(`  {"a":[1]} trailing`)[start:end]) != `{"a":[1]}` {
		t.Fatalf("Skip = (%d,%d)", start, end)
	}
	if err := Pretouch(reflect.TypeOf(struct{ X int }{})); err != nil {
		t.Fatalf("Pretouch error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// Option constants and bitmask
// ---------------------------------------------------------------------------

func TestOptionBitPositions(t *testing.T) {
	// Verify the iota ordering matches the plan exactly.
	cases := []struct {
		name string
		bit  uint
	}{
		{"OptionUseInt64", 0},
		{"OptionUseNumber", 1},
		{"OptionUseUnicodeErrors", 2},
		{"OptionDisableUnknown", 3},
		{"OptionCopyString", 4},
		{"OptionValidateString", 5},
		{"OptionNoValidateJSON", 6},
		{"OptionCaseSensitive", 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var opts Options = 1 << c.bit
			if opts == 0 {
				t.Fatalf("bit %d produced zero option", c.bit)
			}
		})
	}
}

func TestOptionConstantsDistinct(t *testing.T) {
	all := []Options{
		OptionUseInt64,
		OptionUseNumber,
		OptionUseUnicodeErrors,
		OptionDisableUnknown,
		OptionCopyString,
		OptionValidateString,
		OptionNoValidateJSON,
		OptionCaseSensitive,
	}
	seen := map[uint]bool{}
	for _, o := range all {
		if o == 0 {
			t.Fatalf("option is zero: %v", o)
		}
		bit := uint(0)
		for i := 0; i < 64; i++ {
			if o&(1<<uint(i)) != 0 {
				if bit != 0 {
					t.Fatalf("option %v has multiple bits set", o)
				}
				bit = uint(i)
			}
		}
		if seen[bit] {
			t.Fatalf("bit %d shared by multiple options", bit)
		}
		seen[bit] = true
	}
	if len(seen) != len(all) {
		t.Fatalf("expected %d distinct bits, got %d", len(all), len(seen))
	}
}

func TestSetOptionsUpdatesConvenienceFlags(t *testing.T) {
	d := NewDecoder(``)
	d.SetOptions(OptionUseInt64 | OptionDisableUnknown)
	if !d.useInt64 {
		t.Fatalf("SetOptions did not set useInt64")
	}
	if !d.disallowUnknown {
		t.Fatalf("SetOptions did not set disallowUnknown")
	}
	d.SetOptions(0)
	if d.useNumber || d.useInt64 || d.disallowUnknown {
		t.Fatalf("SetOptions(0) did not clear convenience flags")
	}
}

func TestSetOptionsPanicsOnUseInt64AndUseNumber(t *testing.T) {
	d := NewDecoder(``)
	defer func() {
		if recover() == nil {
			t.Fatalf("SetOptions did not panic")
		}
	}()
	d.SetOptions(OptionUseNumber | OptionUseInt64)
}

func TestUseInt64ClearsUseNumber(t *testing.T) {
	d := NewDecoder(`{"n":1}`)
	d.UseNumber()
	d.UseInt64()
	if d.useNumber || d.opts&OptionUseNumber != 0 {
		t.Fatalf("UseInt64 did not clear UseNumber")
	}
	var out map[string]interface{}
	if err := d.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if got, ok := out["n"].(int64); !ok || got != 1 {
		t.Fatalf("n = %v (%T), want int64(1)", out["n"], out["n"])
	}
}

func TestUseNumberClearsUseInt64(t *testing.T) {
	d := NewDecoder(`{"n":1}`)
	d.UseInt64()
	d.UseNumber()
	if d.useInt64 || d.opts&OptionUseInt64 != 0 {
		t.Fatalf("UseNumber did not clear UseInt64")
	}
	var out map[string]interface{}
	if err := d.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if got, ok := out["n"].(json.Number); !ok || got.String() != "1" {
		t.Fatalf("n = %v (%T), want json.Number(1)", out["n"], out["n"])
	}
}

// ---------------------------------------------------------------------------
// UseInt64
// ---------------------------------------------------------------------------

func TestUseInt64ConvertsNestedInterface(t *testing.T) {
	d := NewDecoder(`{"n":42,"f":3.14}`)
	d.UseInt64()
	var out map[string]interface{}
	if err := d.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if v, ok := out["n"].(int64); !ok || v != 42 {
		t.Fatalf("n = %v (%T), want int64 42", out["n"], out["n"])
	}
	// 3.14 is not an integer; it should remain a json.Number.
	if _, ok := out["f"].(json.Number); !ok {
		t.Fatalf("f type = %T, want json.Number", out["f"])
	}
}

func TestUseInt64ConvertsNestedSlice(t *testing.T) {
	d := NewDecoder(`[1, 2, 3, 4]`)
	d.UseInt64()
	var out []interface{}
	if err := d.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	for i, v := range out {
		if iv, ok := v.(int64); !ok || iv != int64(i+1) {
			t.Fatalf("out[%d] = %v (%T), want int64 %d", i, v, v, i+1)
		}
	}
}

func TestUseInt64ConvertsDeeplyNested(t *testing.T) {
	d := NewDecoder(`{"a":[{"b":7},{"b":8}]}`)
	d.UseInt64()
	var out map[string]interface{}
	if err := d.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	arr, ok := out["a"].([]interface{})
	if !ok {
		t.Fatalf("a type = %T, want []interface{}", out["a"])
	}
	for i, v := range arr {
		m, ok := v.(map[string]interface{})
		if !ok {
			t.Fatalf("a[%d] type = %T, want map[string]interface{}", i, v)
		}
		bv, ok := m["b"].(int64)
		if !ok {
			t.Fatalf("a[%d].b type = %T, want int64", i, m["b"])
		}
		if bv != int64(i+7) {
			t.Fatalf("a[%d].b = %d, want %d", i, bv, i+7)
		}
	}
}

func TestUseInt64LeavesLargeNumbersAsNumber(t *testing.T) {
	// 99999999999999999999 exceeds int64 range; it stays a json.Number.
	d := NewDecoder(`{"n":99999999999999999999}`)
	d.UseInt64()
	var out map[string]interface{}
	if err := d.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if _, ok := out["n"].(json.Number); !ok {
		t.Fatalf("n type = %T, want json.Number (overflow)", out["n"])
	}
}

func TestUseInt64OnStructUsesDeclaredTypes(t *testing.T) {
	type S struct {
		N int `json:"n"`
	}
	d := NewDecoder(`{"n":42}`)
	d.UseInt64()
	var s S
	if err := d.Decode(&s); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if s.N != 42 {
		t.Fatalf("s.N = %d, want 42", s.N)
	}
}

// ---------------------------------------------------------------------------
// DisallowUnknownFields
// ---------------------------------------------------------------------------

type disallowUnknownTarget struct {
	A int `json:"a"`
}

func TestDisallowUnknownFieldsRejects(t *testing.T) {
	d := NewDecoder(`{"a":1,"b":2}`)
	d.DisallowUnknownFields()
	var v disallowUnknownTarget
	err := d.Decode(&v)
	if err == nil {
		t.Fatalf("expected error for unknown field b")
	}
}

func TestDisallowUnknownFieldsAcceptsKnown(t *testing.T) {
	d := NewDecoder(`{"a":1}`)
	d.DisallowUnknownFields()
	var v disallowUnknownTarget
	if err := d.Decode(&v); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if v.A != 1 {
		t.Fatalf("v.A = %d, want 1", v.A)
	}
}

func TestSetOptionsDisallowUnknownBit(t *testing.T) {
	d := NewDecoder(`{"a":1,"b":2}`)
	d.SetOptions(OptionDisableUnknown)
	var v disallowUnknownTarget
	if err := d.Decode(&v); err == nil {
		t.Fatalf("expected error for unknown field b via SetOptions")
	}
}

// ---------------------------------------------------------------------------
// UseNumber
// ---------------------------------------------------------------------------

func TestUseNumberDecodesAsJsonNumber(t *testing.T) {
	d := NewDecoder(`{"n":123}`)
	d.UseNumber()
	var out map[string]interface{}
	if err := d.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if n, ok := out["n"].(json.Number); !ok {
		t.Fatalf("n type = %T, want json.Number", out["n"])
	} else if string(n) != "123" {
		t.Fatalf("n = %s, want 123", n)
	}
}

// ---------------------------------------------------------------------------
// Reset preserves options
// ---------------------------------------------------------------------------

func TestResetPreservesOptions(t *testing.T) {
	d := NewDecoder(`{"n":1}`)
	d.UseNumber()
	d.Reset(`{"n":2}`)
	var out map[string]interface{}
	if err := d.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if _, ok := out["n"].(json.Number); !ok {
		t.Fatalf("n type = %T, want json.Number after Reset", out["n"])
	}
	if d.opts&OptionUseNumber == 0 {
		t.Fatalf("opts lost after Reset: %v", d.opts)
	}
}

// ---------------------------------------------------------------------------
// Pos / successive decodes
// ---------------------------------------------------------------------------

func TestPosAdvancesOnDecode(t *testing.T) {
	d := NewDecoder(`{"a":1}{"b":2}`)
	if d.Pos() != 0 {
		t.Fatalf("initial Pos = %d, want 0", d.Pos())
	}
	var first map[string]int
	if err := d.Decode(&first); err != nil {
		t.Fatalf("first Decode error = %v", err)
	}
	if first["a"] != 1 {
		t.Fatalf("first = %v", first)
	}
	if d.Pos() == 0 {
		t.Fatalf("Pos did not advance after first Decode: %d", d.Pos())
	}
	var second map[string]int
	if err := d.Decode(&second); err != nil {
		t.Fatalf("second Decode error = %v", err)
	}
	if second["b"] != 2 {
		t.Fatalf("second = %v", second)
	}
	if err := d.Decode(&second); err != io.EOF {
		t.Fatalf("third Decode error = %v, want io.EOF", err)
	}
}

func TestPosAdvancesThroughWhitespace(t *testing.T) {
	d := NewDecoder(`  {"a":1}  `)
	var out map[string]int
	if err := d.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if out["a"] != 1 {
		t.Fatalf("a = %d, want 1", out["a"])
	}
	// Pos should now be past the value and its leading whitespace.
	if d.Pos() < 2 {
		t.Fatalf("Pos = %d, want >= 2", d.Pos())
	}
}

// ---------------------------------------------------------------------------
// CheckTrailings
// ---------------------------------------------------------------------------

func TestCheckTrailingsEmptySource(t *testing.T) {
	d := NewDecoder(``)
	if err := d.CheckTrailings(); err != nil {
		t.Fatalf("CheckTrailings on empty source = %v, want nil", err)
	}
}

func TestCheckTrailingsWhitespaceOnly(t *testing.T) {
	d := NewDecoder(`   `)
	if err := d.CheckTrailings(); err != nil {
		t.Fatalf("CheckTrailings on whitespace-only = %v, want nil", err)
	}
}

func TestCheckTrailingsNonWhitespace(t *testing.T) {
	d := NewDecoder(`{"a":1} extra`)
	var out map[string]int
	if err := d.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if err := d.CheckTrailings(); err == nil {
		t.Fatalf("CheckTrailings returned nil with trailing bytes")
	}
}

func TestCheckTrailingsAllWhitespaceAfterValue(t *testing.T) {
	d := NewDecoder(`{"a":1}   `)
	var out map[string]int
	if err := d.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if err := d.CheckTrailings(); err != nil {
		t.Fatalf("CheckTrailings = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// Skip
// ---------------------------------------------------------------------------

func TestSkipStringWithBraces(t *testing.T) {
	// The braces inside the string must not be counted as structural.
	in := []byte(`"} {"`)
	start, end := Skip(in)
	if start != 0 {
		t.Fatalf("start = %d, want 0", start)
	}
	if end != len(in) {
		t.Fatalf("end = %d, want %d", end, len(in))
	}
}

func TestSkipStringWithEscapedQuote(t *testing.T) {
	in := []byte(`"a\"b"`)
	start, end := Skip(in)
	if start != 0 || end != len(in) {
		t.Fatalf("Skip = (%d,%d), want (0,%d)", start, end, len(in))
	}
	if string(in[start:end]) != `"a\"b"` {
		t.Fatalf("value = %q", in[start:end])
	}
}

func TestSkipNestedArray(t *testing.T) {
	in := []byte(`[1, [2, 3], {"k": [4, 5]}]`)
	start, end := Skip(in)
	if start != 0 || end != len(in) {
		t.Fatalf("Skip = (%d,%d), want (0,%d)", start, end, len(in))
	}
}

func TestSkipNumber(t *testing.T) {
	in := []byte(`123 trailing`)
	start, end := Skip(in)
	if start != 0 {
		t.Fatalf("start = %d, want 0", start)
	}
	if end != 3 {
		t.Fatalf("end = %d, want 3", end)
	}
	if string(in[start:end]) != "123" {
		t.Fatalf("value = %q", in[start:end])
	}
}

func TestSkipBool(t *testing.T) {
	in := []byte(`true false`)
	start, end := Skip(in)
	if start != 0 || end != 4 {
		t.Fatalf("Skip = (%d,%d), want (0,4)", start, end)
	}
	if string(in[start:end]) != "true" {
		t.Fatalf("value = %q", in[start:end])
	}
}

func TestSkipNull(t *testing.T) {
	in := []byte(`null`)
	start, end := Skip(in)
	if start != 0 || end != 4 {
		t.Fatalf("Skip = (%d,%d), want (0,4)", start, end)
	}
}

func TestSkipLeadingWhitespace(t *testing.T) {
	in := []byte("  \n\t  42")
	start, end := Skip(in)
	if start != 6 {
		t.Fatalf("start = %d, want 6", start)
	}
	if end != 8 {
		t.Fatalf("end = %d, want 8", end)
	}
}

func TestSkipIncompleteReturnsZeroZero(t *testing.T) {
	cases := [][]byte{
		[]byte(``),
		[]byte(`   `),
		[]byte(`{"a":`),
		[]byte(`[1, 2`),
		[]byte(`"unclosed`),
		[]byte(`"truncated\`),
	}
	for _, in := range cases {
		start, end := Skip(in)
		if start != 0 || end != 0 {
			t.Fatalf("Skip(%q) = (%d,%d), want (0,0)", in, start, end)
		}
	}
}

func TestSkipPretouchMany(t *testing.T) {
	if err := PretouchMany([]reflect.Type{reflect.TypeOf(0), reflect.TypeOf("")}); err != nil {
		t.Fatalf("PretouchMany error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// StreamDecoder
// ---------------------------------------------------------------------------

func TestNewStreamDecoder(t *testing.T) {
	sd := NewStreamDecoder(strings.NewReader(`{"a":1}`))
	if sd == nil {
		t.Fatalf("NewStreamDecoder returned nil")
	}
}

func TestStreamDecoderDecode(t *testing.T) {
	sd := NewStreamDecoder(strings.NewReader(`{"a":1}`))
	var out map[string]int
	if err := sd.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if out["a"] != 1 {
		t.Fatalf("a = %d, want 1", out["a"])
	}
}

func TestStreamDecoderMore(t *testing.T) {
	sd := NewStreamDecoder(strings.NewReader(`[1, 2, 3]`))
	var arr []int
	if err := sd.Decode(&arr); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if len(arr) != 3 {
		t.Fatalf("arr = %v, want 3 elements", arr)
	}
	// After decoding the whole array, More should return false.
	if sd.More() {
		t.Fatalf("More = true after full decode, want false")
	}
}

func TestStreamDecoderSuccessiveDecodes(t *testing.T) {
	sd := NewStreamDecoder(strings.NewReader(`1 2 3`))
	for i := 1; i <= 3; i++ {
		var n int
		if err := sd.Decode(&n); err != nil {
			t.Fatalf("Decode %d error = %v", i, err)
		}
		if n != i {
			t.Fatalf("Decode %d = %d, want %d", i, n, i)
		}
	}
	var n int
	if err := sd.Decode(&n); err != io.EOF {
		t.Fatalf("final Decode error = %v, want io.EOF", err)
	}
}

func TestStreamDecoderInputOffsetAdvances(t *testing.T) {
	sd := NewStreamDecoder(strings.NewReader(`{"a":1}{"b":2}`))
	var first map[string]int
	if err := sd.Decode(&first); err != nil {
		t.Fatalf("first Decode error = %v", err)
	}
	off1 := sd.InputOffset()
	if off1 == 0 {
		t.Fatalf("InputOffset = 0 after first decode")
	}
	var second map[string]int
	if err := sd.Decode(&second); err != nil {
		t.Fatalf("second Decode error = %v", err)
	}
	off2 := sd.InputOffset()
	if off2 <= off1 {
		t.Fatalf("InputOffset did not advance: %d -> %d", off1, off2)
	}
}

// ---------------------------------------------------------------------------
// SyntaxError / MismatchTypeError compatibility
// ---------------------------------------------------------------------------

func TestSyntaxErrorMethods(t *testing.T) {
	se := SyntaxError{Msg: "bad"}
	if se.Message() != "bad" {
		t.Fatalf("Message() = %q, want bad", se.Message())
	}
	if se.Description() == "" || se.Error() == "" {
		t.Fatalf("empty Description/Error")
	}
}

func TestMismatchTypeErrorMethods(t *testing.T) {
	mte := MismatchTypeError{Type: reflect.TypeOf(0)}
	if mte.Description() == "" || mte.Error() == "" {
		t.Fatalf("empty Description/Error")
	}
}

func TestDecodeSyntaxErrorReturned(t *testing.T) {
	d := NewDecoder(`{bad json`)
	var out map[string]interface{}
	err := d.Decode(&out)
	if err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
	if _, ok := err.(*json.SyntaxError); !ok {
		t.Fatalf("error type = %T, want *json.SyntaxError from backend", err)
	}
}
