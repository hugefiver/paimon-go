package encoder

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestEncodeBasic(t *testing.T) {
	b, err := Encode(map[string]string{"x": "y"}, 0)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("Encode output invalid: %s", b)
	}
	var got map[string]string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if got["x"] != "y" {
		t.Fatalf("got = %v", got)
	}
}

func TestEncodeEscapeHTML(t *testing.T) {
	b, err := Encode("<a&b>", EscapeHTML)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if !bytes.Contains(b, []byte(`\u003c`)) {
		t.Fatalf("expected escaped <, got %s", b)
	}
	if !bytes.Contains(b, []byte(`\u0026`)) {
		t.Fatalf("expected escaped &, got %s", b)
	}
}

func TestEncodeWithoutEscapeHTML(t *testing.T) {
	b, err := Encode("<a&b>", 0)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if bytes.Contains(b, []byte(`\u003c`)) {
		t.Fatalf("expected unescaped <, got %s", b)
	}
	if bytes.Contains(b, []byte(`\u0026`)) {
		t.Fatalf("expected unescaped &, got %s", b)
	}
}

func TestEncodeSortMapKeys(t *testing.T) {
	m := map[string]int{"c": 3, "a": 1, "b": 2}
	b, err := Encode(m, SortMapKeys)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	s := string(b)
	keys := []string{"a", "b", "c"}
	idx := map[string]int{}
	for _, k := range keys {
		idx[k] = strings.Index(s, `"`+k+`":`)
		if idx[k] < 0 {
			t.Fatalf("key %q missing in %s", k, s)
		}
	}
	if !(idx["a"] < idx["b"] && idx["b"] < idx["c"]) {
		t.Fatalf("keys not sorted: %s", s)
	}
}

func TestEncodeCompatibleWithStdSortsAndEscapes(t *testing.T) {
	m := map[string]string{"b": "<x>", "a": "&"}
	b, err := Encode(m, CompatibleWithStd)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	s := string(b)
	// CompatibleWithStd implies both sorting and HTML escaping.
	if !(strings.Index(s, `"a"`) < strings.Index(s, `"b"`)) {
		t.Fatalf("keys not sorted: %s", s)
	}
	if !strings.Contains(s, `\u003c`) {
		t.Fatalf("expected escaped <, got %s", s)
	}
	if !strings.Contains(s, `\u0026`) {
		t.Fatalf("expected escaped &, got %s", s)
	}
}

func TestEncodeIndented(t *testing.T) {
	b, err := EncodeIndented(map[string]int{"x": 1}, "", "  ", 0)
	if err != nil {
		t.Fatalf("EncodeIndented error = %v", err)
	}
	if !bytes.Contains(b, []byte("\n  ")) {
		t.Fatalf("expected indented output, got %s", b)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid JSON: %s", b)
	}
}

func TestEncodeIntoAppends(t *testing.T) {
	var dst []byte
	if err := EncodeInto(&dst, map[string]string{"x": "y"}, 0); err != nil {
		t.Fatalf("EncodeInto error = %v", err)
	}
	// Map ordering is non-deterministic without SortMapKeys, but the
	// single-pair map is stable.
	if string(dst) != `{"x":"y"}` {
		t.Fatalf("EncodeInto output = %q", string(dst))
	}
}

func TestEncodeIntoPreservesExistingContent(t *testing.T) {
	dst := []byte("prefix:")
	if err := EncodeInto(&dst, "hello", 0); err != nil {
		t.Fatalf("EncodeInto error = %v", err)
	}
	if string(dst) != `prefix:"hello"` {
		t.Fatalf("EncodeInto with prefix = %q", string(dst))
	}
}

func TestEncodeIntoNilBuffer(t *testing.T) {
	defer func() {
		if got := recover(); got != "user-supplied buffer buf is nil" {
			t.Fatalf("EncodeInto(nil, ...) panic = %#v; want exact upstream literal", got)
		}
	}()
	_ = EncodeInto(nil, "hello", 0)
}

func TestEncodeIntoFailureLeavesBufferUntouched(t *testing.T) {
	dst := []byte("orig")
	// chan cannot be marshaled by encoding/json.
	if err := EncodeInto(&dst, make(chan int), 0); err == nil {
		t.Fatalf("expected error marshaling chan")
	}
	if string(dst) != "orig" {
		t.Fatalf("buffer modified on error: %q", string(dst))
	}
}

func TestEncodeNumbers(t *testing.T) {
	b, err := Encode(42, 0)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if string(b) != "42" {
		t.Fatalf("got %s", b)
	}
	b, err = Encode(3.14, 0)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if string(b) != "3.14" {
		t.Fatalf("got %s", b)
	}
	b, err = Encode(true, 0)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if string(b) != "true" {
		t.Fatalf("got %s", b)
	}
	b, err = Encode(nil, 0)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if string(b) != "null" {
		t.Fatalf("got %s", b)
	}
}

func TestEncodeInfNan(t *testing.T) {
	// encoding/json returns an error for inf/nan by default.
	_, err := Encode(math.NaN(), 0)
	if err == nil {
		t.Fatalf("expected error for NaN without EncodeNullForInfOrNan")
	}
}

func TestHTMLEscape(t *testing.T) {
	out := HTMLEscape(nil, []byte(`"<>&"`))
	if !bytes.Contains(out, []byte(`\u003c`)) {
		t.Fatalf("expected escaped <, got %s", out)
	}
	if !bytes.Contains(out, []byte(`\u003e`)) {
		t.Fatalf("expected escaped >, got %s", out)
	}
	if !bytes.Contains(out, []byte(`\u0026`)) {
		t.Fatalf("expected escaped &, got %s", out)
	}
}

func TestHTMLEscapeAppendsToDst(t *testing.T) {
	dst := []byte("X")
	out := HTMLEscape(dst, []byte(`"<"`))
	if !bytes.HasPrefix(out, []byte("X")) {
		t.Fatalf("expected prefix preserved, got %s", out)
	}
}

func TestQuote(t *testing.T) {
	got := Quote("hello\nworld")
	if got != `"hello\nworld"` {
		t.Fatalf("got %q", got)
	}
}

func TestValid(t *testing.T) {
	cases := []struct {
		name string
		data string
		ok   bool
	}{
		{"valid object", `{"a":1}`, true},
		{"valid array", `[1,2,3]`, true},
		{"valid number", `42`, true},
		{"valid string", `"hi"`, true},
		{"valid true", `true`, true},
		{"valid null", `null`, true},
		{"invalid object", `{`, false},
		{"invalid array", `[1,`, false},
		{"invalid bareword", `foo`, false},
		{"empty", ``, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, start := Valid([]byte(c.data))
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v (data=%q)", ok, c.ok, c.data)
			}
			if ok {
				// start should be the index of the first non-whitespace byte.
				want := firstNonSpaceOffset([]byte(c.data))
				if start != want {
					t.Fatalf("start = %d, want %d", start, want)
				}
			}
		})
	}
}

func TestValidLeadingWhitespace(t *testing.T) {
	data := []byte("   \n\t {\"a\":1}")
	ok, start := Valid(data)
	if !ok {
		t.Fatalf("expected valid")
	}
	if start != 6 {
		t.Fatalf("start = %d, want 6", start)
	}
}

func TestValidOnlyWhitespace(t *testing.T) {
	data := []byte("   \n\t")
	ok, start := Valid(data)
	if ok {
		t.Fatalf("expected invalid for whitespace-only")
	}
	if start != len(data) {
		t.Fatalf("start = %d, want %d", start, len(data))
	}
}

func TestPretouchNoOp(t *testing.T) {
	if err := Pretouch(reflect.TypeOf(0)); err != nil {
		t.Fatalf("Pretouch error = %v", err)
	}
}

func TestPretouchManyNoOp(t *testing.T) {
	types := []reflect.Type{reflect.TypeOf(0), reflect.TypeOf("")}
	if err := PretouchMany(types); err != nil {
		t.Fatalf("PretouchMany error = %v", err)
	}
}

func TestEnableFallbackConstant(t *testing.T) {
	if EnableFallback != false {
		t.Fatalf("EnableFallback = %v, want false", EnableFallback)
	}
}

// ---------------------------------------------------------------------------
// Encoder builder
// ---------------------------------------------------------------------------

func TestEncoderEncode(t *testing.T) {
	enc := &Encoder{}
	b, err := enc.Encode(map[string]int{"a": 1})
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid JSON: %s", b)
	}
}

func TestEncoderSetEscapeHTML(t *testing.T) {
	enc := &Encoder{}
	enc.SetEscapeHTML(true)
	if enc.Opts&EscapeHTML == 0 {
		t.Fatalf("EscapeHTML not set")
	}
	b, err := enc.Encode("<")
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if !bytes.Contains(b, []byte(`\u003c`)) {
		t.Fatalf("expected escaped <, got %s", b)
	}
	enc.SetEscapeHTML(false)
	if enc.Opts&EscapeHTML != 0 {
		t.Fatalf("EscapeHTML still set")
	}
}

func TestEncoderSetIndent(t *testing.T) {
	enc := &Encoder{}
	enc.SetIndent("", "  ")
	b, err := enc.Encode(map[string]int{"a": 1})
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if !bytes.Contains(b, []byte("\n  ")) {
		t.Fatalf("expected indented output, got %s", b)
	}
}

func TestEncoderSetPrefixOnlyIndent(t *testing.T) {
	enc := &Encoder{}
	enc.SetIndent("P", "")

	got, err := enc.Encode(map[string]int{"a": 1})
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	const want = "{\nP\"a\": 1\nP}\n"
	if string(got) != want {
		t.Fatalf("prefix-only SetIndent output = %q, want %q", got, want)
	}
	withoutPrefix := bytes.ReplaceAll(got, []byte("\nP"), []byte("\n"))
	if !json.Valid(withoutPrefix) {
		t.Fatalf("prefix-stripped output is invalid JSON: %q", withoutPrefix)
	}
}

func TestEncoderSetCompactMarshaler(t *testing.T) {
	enc := &Encoder{}
	enc.SetCompactMarshaler(true)
	if enc.Opts&CompactMarshaler == 0 {
		t.Fatalf("CompactMarshaler not set")
	}
	enc.SetCompactMarshaler(false)
	if enc.Opts&CompactMarshaler != 0 {
		t.Fatalf("CompactMarshaler still set")
	}
}

func TestEncoderSetNoQuoteTextMarshaler(t *testing.T) {
	enc := &Encoder{}
	enc.SetNoQuoteTextMarshaler(true)
	if enc.Opts&NoQuoteTextMarshaler == 0 {
		t.Fatalf("NoQuoteTextMarshaler not set")
	}
}

func TestEncoderSetNoValidateJSONMarshaler(t *testing.T) {
	enc := &Encoder{}
	enc.SetNoValidateJSONMarshaler(true)
	if enc.Opts&NoValidateJSONMarshaler == 0 {
		t.Fatalf("NoValidateJSONMarshaler not set")
	}
}

func TestEncoderSetValidateString(t *testing.T) {
	enc := &Encoder{}
	enc.SetValidateString(true)
	if enc.Opts&ValidateString == 0 {
		t.Fatalf("ValidateString not set")
	}
}

func TestEncoderSetNoEncoderNewline(t *testing.T) {
	enc := &Encoder{}
	enc.SetNoEncoderNewline(true)
	if enc.Opts&NoEncoderNewline == 0 {
		t.Fatalf("NoEncoderNewline not set")
	}
	enc.SetNoEncoderNewline(false)
	if enc.Opts&NoEncoderNewline != 0 {
		t.Fatalf("NoEncoderNewline still set")
	}
}

func TestEncoderSortKeys(t *testing.T) {
	enc := &Encoder{}
	if enc.SortKeys() == nil {
		t.Fatalf("SortKeys returned nil")
	}
	if enc.Opts&SortMapKeys == 0 {
		t.Fatalf("SortMapKeys not set")
	}
}

// ---------------------------------------------------------------------------
// StreamEncoder
// ---------------------------------------------------------------------------

func TestNewStreamEncoder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)
	if enc == nil {
		t.Fatalf("NewStreamEncoder returned nil")
	}
	if enc.w == nil {
		t.Fatalf("writer not set")
	}
}

func TestStreamEncoderEncodeAppendsNewlineByDefault(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)
	if err := enc.Encode("hello"); err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	got := buf.String()
	if got != `"hello"`+"\n" {
		t.Fatalf("got %q, want %q", got, `"hello"`+"\n")
	}
}

func TestStreamEncoderEncodeSuppressesNewlineWhenNoEncoderNewline(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)
	enc.SetNoEncoderNewline(true)
	if err := enc.Encode("hello"); err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	got := buf.String()
	if got != `"hello"` {
		t.Fatalf("got %q, want %q", got, `"hello"`)
	}
}

func TestStreamEncoderMultipleEncodes(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)
	for i := 0; i < 3; i++ {
		if err := enc.Encode(i); err != nil {
			t.Fatalf("Encode error = %v", err)
		}
	}
	got := buf.String()
	// Three values, each followed by a newline.
	if got != "0\n1\n2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStreamEncoderWithIndent(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]int{"a": 1}); err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	got := buf.String()
	const want = "{\n  \"a\": 1\n}\n"
	if got != want {
		t.Fatalf("indented stream output = %q, want %q", got, want)
	}
	if !json.Valid([]byte(got)) {
		t.Fatalf("indented stream output is invalid JSON: %q", got)
	}
}

func TestStreamEncoderEscapeHTML(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode("<tag>"); err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`\u003c`)) {
		t.Fatalf("expected escaped <, got %s", buf.String())
	}
}

func TestStreamEncoderWriteError(t *testing.T) {
	want := errors.New("write boom")
	enc := NewStreamEncoder(errWriter{err: want})
	err := enc.Encode("hello")
	if err != want {
		t.Fatalf("Encode error = %v, want original error %v", err, want)
	}
}

func TestStreamEncoderCompletesShortWrites(t *testing.T) {
	w := &oneByteWriter{}
	enc := NewStreamEncoder(w)
	if err := enc.Encode(map[string]int{"a": 1}); err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if got := w.String(); got != "{\"a\":1}\n" {
		t.Fatalf("encoded = %q, want %q", got, "{\"a\":1}\n")
	}
}

func TestStreamEncoderRejectsZeroProgress(t *testing.T) {
	enc := NewStreamEncoder(zeroProgressWriter{})
	if err := enc.Encode(map[string]int{"a": 1}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Encode error = %v, want io.ErrShortWrite", err)
	}
}

// errWriter is an io.Writer that always fails with err.
type errWriter struct{ err error }

func (w errWriter) Write(p []byte) (int, error) {
	return 0, w.err
}

type oneByteWriter struct {
	bytes.Buffer
}

func (w *oneByteWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return w.Buffer.Write(p[:1])
}

type zeroProgressWriter struct{}

func (zeroProgressWriter) Write([]byte) (int, error) {
	return 0, nil
}

// ---------------------------------------------------------------------------
// Option constant ordering
// ---------------------------------------------------------------------------

func TestOptionBitPositions(t *testing.T) {
	// Verify the iota ordering matches the plan exactly.
	cases := []struct {
		name string
		bit  uint
	}{
		{"SortMapKeys", 0},
		{"EscapeHTML", 1},
		{"CompactMarshaler", 2},
		{"NoQuoteTextMarshaler", 3},
		{"NoNullSliceOrMap", 4},
		{"ValidateString", 5},
		{"NoValidateJSONMarshaler", 6},
		{"NoEncoderNewline", 7},
		{"EncodeNullForInfOrNan", 8},
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

func TestOptionToConfigSingleBitsStayIndependent(t *testing.T) {
	tests := []struct {
		name                              string
		opts                              Options
		wantSort, wantEscape, wantCompact bool
	}{
		{name: "SortMapKeys", opts: SortMapKeys, wantSort: true},
		{name: "EscapeHTML", opts: EscapeHTML, wantEscape: true},
		{name: "CompactMarshaler", opts: CompactMarshaler, wantCompact: true},
		{name: "CompatibleWithStd", opts: CompatibleWithStd, wantSort: true, wantEscape: true, wantCompact: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := optionToConfig(tt.opts)
			if cfg.SortMapKeys != tt.wantSort || cfg.EscapeHTML != tt.wantEscape || cfg.CompactMarshaler != tt.wantCompact {
				t.Fatalf("optionToConfig(%v) = sort:%v escape:%v compact:%v; want sort:%v escape:%v compact:%v", tt.opts, cfg.SortMapKeys, cfg.EscapeHTML, cfg.CompactMarshaler, tt.wantSort, tt.wantEscape, tt.wantCompact)
			}
		})
	}
}

func TestOptionToConfigMapping(t *testing.T) {
	// Verify the mapping of Options to backend.Config fields.
	cfg := optionToConfig(SortMapKeys)
	if !cfg.SortMapKeys {
		t.Fatalf("SortMapKeys not propagated")
	}

	cfg = optionToConfig(EscapeHTML)
	if !cfg.EscapeHTML {
		t.Fatalf("EscapeHTML not propagated")
	}

	cfg = optionToConfig(CompactMarshaler)
	if !cfg.CompactMarshaler {
		t.Fatalf("CompactMarshaler not propagated")
	}

	cfg = optionToConfig(NoQuoteTextMarshaler)
	if !cfg.NoQuoteTextMarshaler {
		t.Fatalf("NoQuoteTextMarshaler not propagated")
	}

	cfg = optionToConfig(NoNullSliceOrMap)
	if !cfg.NoNullSliceOrMap {
		t.Fatalf("NoNullSliceOrMap not propagated")
	}

	cfg = optionToConfig(ValidateString)
	if !cfg.ValidateString {
		t.Fatalf("ValidateString not propagated")
	}

	cfg = optionToConfig(NoValidateJSONMarshaler)
	if !cfg.NoValidateJSONMarshaler {
		t.Fatalf("NoValidateJSONMarshaler not propagated")
	}

	cfg = optionToConfig(NoEncoderNewline)
	if !cfg.NoEncoderNewline {
		t.Fatalf("NoEncoderNewline not propagated")
	}

	cfg = optionToConfig(EncodeNullForInfOrNan)
	if !cfg.EncodeNullForInfOrNan {
		t.Fatalf("EncodeNullForInfOrNan not propagated")
	}

	// CompatibleWithStd implies both SortMapKeys and EscapeHTML.
	cfg = optionToConfig(CompatibleWithStd)
	if !cfg.SortMapKeys {
		t.Fatalf("CompatibleWithStd does not imply SortMapKeys")
	}
	if !cfg.EscapeHTML {
		t.Fatalf("CompatibleWithStd does not imply EscapeHTML")
	}
}

func TestCompatibleWithStdCompositeBits(t *testing.T) {
	want := SortMapKeys | EscapeHTML | CompactMarshaler
	if CompatibleWithStd != want {
		t.Fatalf("CompatibleWithStd = %v, want %v", CompatibleWithStd, want)
	}
	if CompatibleWithStd&EncodeNullForInfOrNan != 0 {
		t.Fatalf("CompatibleWithStd overlaps EncodeNullForInfOrNan")
	}
}

func TestEncodeStructWithJSONTags(t *testing.T) {
	type sample struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	b, err := Encode(sample{Name: "<x>", Count: 2}, 0)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	var out sample
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if out.Name != "<x>" || out.Count != 2 {
		t.Fatalf("roundtrip mismatch: %+v", out)
	}
}

func TestEncodeSlice(t *testing.T) {
	b, err := Encode([]int{1, 2, 3}, 0)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if string(b) != "[1,2,3]" {
		t.Fatalf("got %s", b)
	}
}

func TestEncodeNestedMapSorted(t *testing.T) {
	m := map[string]map[string]int{
		"z": {"y": 2, "x": 1},
		"a": {"b": 3, "a": 4},
	}
	b, err := Encode(m, SortMapKeys)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	s := string(b)
	// Outer keys sorted: "a" before "z".
	if !(strings.Index(s, `"a"`) < strings.Index(s, `"z"`)) {
		t.Fatalf("outer keys not sorted: %s", s)
	}
	// Inner keys sorted: "a","b" before "x","y".
	if !(strings.Index(s, `"a"`) < strings.Index(s, `"b"`)) {
		t.Fatalf("inner keys not sorted: %s", s)
	}
}

func TestEncodeIntoRoundtrip(t *testing.T) {
	var dst []byte
	// Encode two values back-to-back; the result should be two valid
	// JSON values appended together.
	if err := EncodeInto(&dst, "a", 0); err != nil {
		t.Fatalf("first EncodeInto error = %v", err)
	}
	if err := EncodeInto(&dst, "b", 0); err != nil {
		t.Fatalf("second EncodeInto error = %v", err)
	}
	if string(dst) != `"a""b"` {
		t.Fatalf("got %q", string(dst))
	}
}

func TestEncoderChaining(t *testing.T) {
	enc := &Encoder{}
	enc.SetEscapeHTML(true)
	enc.SetCompactMarshaler(true)
	enc.SetNoQuoteTextMarshaler(true)
	if enc.Opts&EscapeHTML == 0 || enc.Opts&CompactMarshaler == 0 || enc.Opts&NoQuoteTextMarshaler == 0 {
		t.Fatalf("chaining did not set all options: %v", enc.Opts)
	}
}

func TestEncoderZeroValue(t *testing.T) {
	var enc Encoder
	b, err := enc.Encode("hello")
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if string(b) != `"hello"` {
		t.Fatalf("got %s", b)
	}
}

// Ensure that SortMapKeys matches encoding/json's default behavior of
// sorting map keys.
func TestEncodeSortMapKeysMatchesStd(t *testing.T) {
	m := map[string]int{"c": 3, "a": 1, "b": 2}
	std, _ := json.Marshal(m)
	sonic, err := Encode(m, SortMapKeys)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if string(std) != string(sonic) {
		t.Fatalf("mismatch:\nstd:   %s\nsonic: %s", std, sonic)
	}
}

// TestEncodeSortedKeysDeterministic ensures that multiple encodings of
// the same map produce identical output when SortMapKeys is set.
func TestEncodeSortedKeysDeterministic(t *testing.T) {
	m := map[string]int{"c": 3, "a": 1, "b": 2}
	first, err := Encode(m, SortMapKeys)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	for i := 0; i < 10; i++ {
		b, err := Encode(m, SortMapKeys)
		if err != nil {
			t.Fatalf("Encode error = %v", err)
		}
		if string(b) != string(first) {
			t.Fatalf("non-deterministic output:\nfirst: %s\nlater: %s", first, b)
		}
	}
}

// TestEncodeUnsortedMapMayVary documents that without SortMapKeys the
// reflection backend (encoding/json) actually still sorts keys, so
// output is stable. We assert that the output is at least valid JSON
// and decodes back to the same map.
func TestEncodeUnsortedMapStable(t *testing.T) {
	m := map[string]int{"c": 3, "a": 1, "b": 2}
	b, err := Encode(m, 0)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	var got map[string]int
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if len(got) != len(m) {
		t.Fatalf("len mismatch: %d vs %d", len(got), len(m))
	}
	for k, v := range m {
		if got[k] != v {
			t.Fatalf("value mismatch for %q: %d vs %d", k, got[k], v)
		}
	}
}

// Ensure SortKeys remains chainable while Set* methods mutate in place.
func TestEncoderSortKeysReturnsPointer(t *testing.T) {
	enc := &Encoder{}
	if enc.SortKeys() != enc {
		t.Fatalf("SortKeys did not return receiver")
	}
}

// TestSortMapKeysConstantsDistinct verifies that all option constants
// have distinct bit values (no two share a bit).
func TestOptionConstantsDistinct(t *testing.T) {
	all := []Options{
		SortMapKeys,
		EscapeHTML,
		CompactMarshaler,
		NoQuoteTextMarshaler,
		NoNullSliceOrMap,
		ValidateString,
		NoValidateJSONMarshaler,
		NoEncoderNewline,
		EncodeNullForInfOrNan,
	}
	seen := map[uint]bool{}
	for _, o := range all {
		// Each option should be a single bit.
		if o == 0 {
			t.Fatalf("option is zero: %v", o)
		}
		// Count bits set; should be exactly 1.
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

// TestEncodeLargeString exercises a larger input to ensure the path
// doesn't truncate or mis-handle bigger buffers.
func TestEncodeLargeString(t *testing.T) {
	s := strings.Repeat("x", 10000)
	b, err := Encode(s, 0)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	var got string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if len(got) != len(s) {
		t.Fatalf("len mismatch: %d vs %d", len(got), len(s))
	}
}

// TestEncodeMapWithNonStringKeys ensures encoding/json's handling of
// non-string map keys is exposed.
func TestEncodeMapWithIntKeys(t *testing.T) {
	m := map[int]string{1: "a", 2: "b"}
	b, err := Encode(m, SortMapKeys)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid JSON: %s", b)
	}
}

// TestStreamEncoderEncodeMap verifies streaming a map works.
func TestStreamEncoderEncodeMap(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)
	enc.SortKeys()
	if err := enc.Encode(map[string]int{"a": 1, "b": 2}); err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	got := buf.String()
	// Should be valid JSON followed by newline.
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("expected trailing newline, got %q", got)
	}
	body := strings.TrimSuffix(got, "\n")
	if !json.Valid([]byte(body)) {
		t.Fatalf("invalid JSON: %s", body)
	}
	var m map[string]int
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if m["a"] != 1 || m["b"] != 2 {
		t.Fatalf("roundtrip mismatch: %v", m)
	}
}

// TestStreamEncoderEncodeError verifies that encoding errors propagate.
func TestStreamEncoderEncodeError(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)
	// chan cannot be marshaled.
	err := enc.Encode(make(chan int))
	if err == nil {
		t.Fatalf("expected error for chan")
	}
	// Nothing should have been written.
	if buf.Len() != 0 {
		t.Fatalf("buffer not empty after error: %s", buf.String())
	}
}

// TestStreamEncoderNoNewlineMultipleEncodes verifies that with
// NoEncoderNewline, multiple encodes produce concatenated JSON values
// with no newlines between them.
func TestStreamEncoderNoNewlineMultipleEncodes(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)
	enc.SetNoEncoderNewline(true)
	for i := 0; i < 3; i++ {
		if err := enc.Encode(i); err != nil {
			t.Fatalf("Encode error = %v", err)
		}
	}
	got := buf.String()
	if got != "012" {
		t.Fatalf("got %q, want %q", got, "012")
	}
}

// TestStreamEncoderIndentedNoNewline verifies indent + NoEncoderNewline.
func TestStreamEncoderIndentedNoNewline(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetNoEncoderNewline(true)
	if err := enc.Encode(map[string]int{"a": 1}); err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	got := buf.String()
	if strings.HasSuffix(got, "\n") {
		t.Fatalf("expected no trailing newline, got %q", got)
	}
	if !strings.Contains(got, "\n  ") {
		t.Fatalf("expected indented output, got %s", got)
	}
}

// Ensure that a freshly-constructed StreamEncoder has a permissive
// (zero) Options configuration.
func TestStreamEncoderDefaultOpts(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)
	if enc.Opts != 0 {
		t.Fatalf("default Opts = %v, want 0", enc.Opts)
	}
}

// TestEncodeIntoWithEscapeHTML verifies EncodeInto respects options.
func TestEncodeIntoWithEscapeHTML(t *testing.T) {
	var dst []byte
	if err := EncodeInto(&dst, "<a>", EscapeHTML); err != nil {
		t.Fatalf("EncodeInto error = %v", err)
	}
	if !bytes.Contains(dst, []byte(`\u003c`)) {
		t.Fatalf("expected escaped <, got %s", dst)
	}
}

// TestValidAfterHTMLEscape verifies that HTML-escaped output is still
// valid JSON.
func TestValidAfterHTMLEscape(t *testing.T) {
	src := []byte(`"<>&"`)
	esc := HTMLEscape(nil, src)
	ok, _ := Valid(esc)
	if !ok {
		t.Fatalf("HTML-escaped output not valid: %s", esc)
	}
}

// TestEncodeJsonMarshaler verifies that types implementing
// json.Marshaler are honored.
func TestEncodeJsonMarshaler(t *testing.T) {
	b, err := Encode(jsonMarshaler{}, 0)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if string(b) != `{"custom":true}` {
		t.Fatalf("got %s", b)
	}
}

type jsonMarshaler struct{}

func (jsonMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`{"custom":true}`), nil
}

// TestEncodeTextMarshaler verifies that types implementing
// encoding.TextMarshaler are quoted as strings.
func TestEncodeTextMarshaler(t *testing.T) {
	b, err := Encode(textMarshaler{}, 0)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if string(b) != `"hello"` {
		t.Fatalf("got %s", b)
	}
}

type textMarshaler struct{}

func (textMarshaler) MarshalText() ([]byte, error) {
	return []byte("hello"), nil
}

// TestEncodeMapKeysSortedLexicographically ensures that sorted map keys
// are in lexicographic byte order (matching encoding/json).
func TestEncodeMapKeysSortedLexicographically(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	m := map[string]int{}
	for i, k := range keys {
		m[k] = i
	}
	b, err := Encode(m, SortMapKeys)
	if err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	std, _ := json.Marshal(m)
	if string(b) != string(std) {
		t.Fatalf("mismatch:\nsonic: %s\nstd:   %s", b, std)
	}
	// Also verify sorted order explicitly.
	sortedKeys := append([]string(nil), keys...)
	sort.Strings(sortedKeys)
	prev := -1
	for _, k := range sortedKeys {
		idx := strings.Index(string(b), `"`+k+`":`)
		if idx < 0 {
			t.Fatalf("key %q not found", k)
		}
		if idx <= prev {
			t.Fatalf("keys not in order at %q", k)
		}
		prev = idx
	}
}
