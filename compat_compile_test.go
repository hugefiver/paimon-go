package sonic_test

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/decoder"
	"github.com/bytedance/sonic/encoder"
	"github.com/bytedance/sonic/fastjson"
	"github.com/bytedance/sonic/option"
	"github.com/bytedance/sonic/stdjsonv2"
	"github.com/bytedance/sonic/unquote"
	"github.com/bytedance/sonic/utf8"
)

// TestCompileCompatibilityRoot is a compile-only fixture that exercises the
// full Sonic v1.15.2 public surface so that signature drift is caught at
// build time. It performs no behavioral assertions beyond what is required to
// keep the references live.
func TestCompileCompatibilityRoot(t *testing.T) {
	_ = sonic.UseStdJSON
	_ = sonic.UseSonicJSON
	if sonic.APIKind != sonic.UseSonicJSON {
		t.Fatalf("APIKind = %v, want %v", sonic.APIKind, sonic.UseSonicJSON)
	}

	// Full Config literal covering all 16 fields.
	cfg := sonic.Config{
		EscapeHTML:              true,
		SortMapKeys:             true,
		CompactMarshaler:        true,
		NoQuoteTextMarshaler:    true,
		NoNullSliceOrMap:        true,
		UseInt64:                true,
		UseNumber:               true,
		UseUnicodeErrors:        true,
		DisallowUnknownFields:   true,
		CopyString:              true,
		ValidateString:          true,
		NoValidateJSONMarshaler: true,
		NoValidateJSONSkip:      true,
		NoEncoderNewline:        true,
		EncodeNullForInfOrNan:   true,
		CaseSensitive:           true,
	}
	// This fixture uses cfg to exercise API methods below. Keep its full field
	// coverage while avoiding the documented incompatible decoder modes.
	cfg.UseInt64 = false
	api := cfg.Froze()
	_ = sonic.ConfigDefault
	_ = sonic.ConfigStd
	_ = sonic.ConfigFastest

	// Root package funcs.
	val := map[string]int{"a": 1}
	if b, err := sonic.Marshal(val); err != nil || !json.Valid(b) {
		t.Fatalf("Marshal: %v %s", err, b)
	}
	if s, err := sonic.MarshalString(val); err != nil || !json.Valid([]byte(s)) {
		t.Fatalf("MarshalString: %v %q", err, s)
	}
	if b, err := sonic.MarshalIndent(val, "", "  "); err != nil || !json.Valid(b) {
		t.Fatalf("MarshalIndent: %v %s", err, b)
	}
	var out map[string]int
	if err := sonic.Unmarshal([]byte(`{"a":1}`), &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if err := sonic.UnmarshalString(`{"a":1}`, &out); err != nil {
		t.Fatalf("UnmarshalString: %v", err)
	}
	if !sonic.Valid([]byte(`{"a":1}`)) {
		t.Fatalf("Valid failed")
	}
	if !sonic.ValidString(`{"a":1}`) {
		t.Fatalf("ValidString failed")
	}
	if _, err := sonic.Get([]byte(`{"a":1}`), "a"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, err := sonic.GetFromString(`{"a":1}`, "a"); err != nil {
		t.Fatalf("GetFromString: %v", err)
	}
	if _, err := sonic.GetCopyFromString(`{"a":1}`, "a"); err != nil {
		t.Fatalf("GetCopyFromString: %v", err)
	}
	if _, err := sonic.GetWithOptions([]byte(`{"a":1}`), ast.SearchOptions{}, "a"); err != nil {
		t.Fatalf("GetWithOptions: %v", err)
	}
	if err := sonic.Pretouch(reflect.TypeOf(map[string]int{})); err != nil {
		t.Fatalf("Pretouch: %v", err)
	}
	if err := sonic.PretouchMany([]reflect.Type{reflect.TypeOf(map[string]int{})}); err != nil {
		t.Fatalf("PretouchMany: %v", err)
	}

	// API methods.
	if s, err := api.MarshalToString(val); err != nil || !json.Valid([]byte(s)) {
		t.Fatalf("MarshalToString: %v %q", err, s)
	}
	if b, err := api.Marshal(val); err != nil || !json.Valid(b) {
		t.Fatalf("API.Marshal: %v %s", err, b)
	}
	if b, err := api.MarshalIndent(val, "", "  "); err != nil || !json.Valid(b) {
		t.Fatalf("API.MarshalIndent: %v %s", err, b)
	}
	var dec2 map[string]int
	if err := api.UnmarshalFromString(`{"a":1}`, &dec2); err != nil {
		t.Fatalf("UnmarshalFromString: %v", err)
	}
	if err := api.Unmarshal([]byte(`{"a":1}`), &dec2); err != nil {
		t.Fatalf("API.Unmarshal: %v", err)
	}
	if !api.Valid([]byte(`{"a":1}`)) {
		t.Fatalf("API.Valid failed")
	}

	// Encoder / Decoder construction via API.
	var buf bytes.Buffer
	enc := api.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	enc.SetIndent("", "  ")
	if err := enc.Encode(val); err != nil {
		t.Fatalf("Encoder.Encode: %v", err)
	}
	rdr := strings.NewReader(`{"a":1}`)
	dec := api.NewDecoder(rdr)
	dec.UseNumber()
	dec.DisallowUnknownFields()
	var v map[string]interface{}
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("Decoder.Decode: %v", err)
	}
	// After decoding a single complete value, More should be false.
	if dec.More() {
		t.Fatalf("More should be false after single value")
	}
	if _, ok := v["n"].(json.Number); ok {
		t.Fatalf("expected non-number")
	}
	// Buffered returns remaining reader.
	_ = dec.Buffered()

	// NoCopyRawMessage.
	raw := sonic.NoCopyRawMessage(`{"x":1}`)
	if b, err := raw.MarshalJSON(); err != nil || !bytes.Equal(b, []byte(`{"x":1}`)) {
		t.Fatalf("NoCopyRawMessage.MarshalJSON: %v %s", err, b)
	}
	var raw2 sonic.NoCopyRawMessage
	if err := raw2.UnmarshalJSON([]byte(`{"x":2}`)); err != nil {
		t.Fatalf("NoCopyRawMessage.UnmarshalJSON: %v", err)
	}
}

// TestCompileCompatibilityEncoder exercises the encoder package surface.
func TestCompileCompatibilityEncoder(t *testing.T) {
	_ = encoder.EnableFallback
	_ = encoder.SortMapKeys
	_ = encoder.EscapeHTML
	_ = encoder.CompactMarshaler
	_ = encoder.NoQuoteTextMarshaler
	_ = encoder.NoNullSliceOrMap
	_ = encoder.ValidateString
	_ = encoder.NoValidateJSONMarshaler
	_ = encoder.NoEncoderNewline
	_ = encoder.CompatibleWithStd
	_ = encoder.EncodeNullForInfOrNan

	if b, err := encoder.Encode(map[string]int{"a": 1}, 0); err != nil || !json.Valid(b) {
		t.Fatalf("Encode: %v %s", err, b)
	}
	if b, err := encoder.EncodeIndented(map[string]int{"a": 1}, "", "  ", 0); err != nil || !json.Valid(b) {
		t.Fatalf("EncodeIndented: %v %s", err, b)
	}
	var encBuf []byte
	if err := encoder.EncodeInto(&encBuf, map[string]int{"a": 1}, 0); err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	if got := encoder.HTMLEscape(nil, []byte(`<a>`)); !bytes.Contains(got, []byte(`\u003c`)) {
		t.Fatalf("HTMLEscape: %s", got)
	}
	if got := encoder.Quote("hello\n"); got != `"hello\n"` {
		t.Fatalf("Quote: %q", got)
	}
	if ok, _ := encoder.Valid([]byte(`{"a":1}`)); !ok {
		t.Fatalf("encoder.Valid failed")
	}
	if err := encoder.Pretouch(reflect.TypeOf(map[string]int{})); err != nil {
		t.Fatalf("encoder.Pretouch: %v", err)
	}
	if err := encoder.PretouchMany([]reflect.Type{reflect.TypeOf(map[string]int{})}); err != nil {
		t.Fatalf("encoder.PretouchMany: %v", err)
	}

	enc := &encoder.Encoder{Opts: encoder.SortMapKeys}
	enc.SetCompactMarshaler(true)
	enc.SetEscapeHTML(true)
	enc.SetIndent("", "  ")
	enc.SetNoEncoderNewline(true)
	enc.SetNoQuoteTextMarshaler(true)
	enc.SetNoValidateJSONMarshaler(true)
	enc.SetValidateString(true)
	enc.SortKeys()
	if b, err := enc.Encode(map[string]int{"a": 1}); err != nil || !json.Valid(b) {
		t.Fatalf("Encoder.Encode: %v %s", err, b)
	}

	var w bytes.Buffer
	se := encoder.NewStreamEncoder(&w)
	if err := se.Encode(map[string]int{"a": 1}); err != nil {
		t.Fatalf("StreamEncoder.Encode: %v", err)
	}
}

// TestCompileCompatibilityDecoder exercises the decoder package surface.
func TestCompileCompatibilityDecoder(t *testing.T) {
	_ = decoder.OptionUseInt64
	_ = decoder.OptionUseNumber
	_ = decoder.OptionUseUnicodeErrors
	_ = decoder.OptionDisableUnknown
	_ = decoder.OptionCopyString
	_ = decoder.OptionValidateString
	_ = decoder.OptionNoValidateJSON
	_ = decoder.OptionCaseSensitive

	decSE := decoder.SyntaxError{Msg: "bad"}
	_ = decSE.Error()
	_ = decSE.Description()
	_ = decSE.Message()
	decMTE := decoder.MismatchTypeError{Type: reflect.TypeOf(0)}
	_ = decMTE.Error()
	_ = decMTE.Description()

	// Skip returns two values.
	start, end := decoder.Skip([]byte(`{"a":1}`))
	if start != 0 || end < start || end > 7 {
		t.Fatalf("Skip: start=%d end=%d", start, end)
	}

	d := decoder.NewDecoder(`{"a":1}`)
	d.SetOptions(decoder.OptionUseNumber)
	d.UseInt64()
	d.UseNumber()
	d.UseUnicodeErrors()
	d.ValidateString()
	d.CopyString()
	d.DisallowUnknownFields()
	var v map[string]interface{}
	if err := d.Decode(&v); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if p := d.Pos(); p < 0 {
		t.Fatalf("Pos: %d", p)
	}
	if err := d.CheckTrailings(); err != nil {
		// Trailing data may be present; not fatal.
		_ = err
	}
	d.Reset(`{"b":2}`)
	var v2 map[string]interface{}
	if err := d.Decode(&v2); err != nil {
		t.Fatalf("Decode after Reset: %v", err)
	}

	r := strings.NewReader(`{"a":1}{"b":2}`)
	sd := decoder.NewStreamDecoder(r)
	var sv map[string]interface{}
	if err := sd.Decode(&sv); err != nil {
		t.Fatalf("StreamDecoder.Decode: %v", err)
	}
	_ = sd.Buffered()
	_ = sd.InputOffset()
	_ = sd.More()

	if err := decoder.Pretouch(reflect.TypeOf(map[string]int{})); err != nil {
		t.Fatalf("decoder.Pretouch: %v", err)
	}
	if err := decoder.PretouchMany([]reflect.Type{reflect.TypeOf(map[string]int{})}); err != nil {
		t.Fatalf("decoder.PretouchMany: %v", err)
	}
}

// TestCompileCompatibilityOption exercises the option package surface.
func TestCompileCompatibilityOption(t *testing.T) {
	_ = option.DefaultDecoderBufferSize
	_ = option.DefaultEncoderBufferSize
	_ = option.DefaultAstBufferSize
	_ = option.LimitBufferSize
	origInline := option.DefaultMaxInlineDepth
	origRecursive := option.DefaultRecursiveDepth
	option.DefaultMaxInlineDepth = origInline
	option.DefaultRecursiveDepth = origRecursive

	co := option.DefaultCompileOptions()
	if co.MaxInlineDepth != option.DefaultMaxInlineDepth {
		t.Fatalf("MaxInlineDepth = %v", co.MaxInlineDepth)
	}
	_ = option.WithCompileMaxInlineDepth(5)
	_ = option.WithCompileRecursiveDepth(2)
	_ = option.WithCompileEncOnlyOmitNull(true)
}

// TestCompileCompatibilityFastjson exercises the fastjson compatibility aliases.
func TestCompileCompatibilityFastjson(t *testing.T) {
	_ = fastjson.ConfigDefault
	_ = fastjson.ConfigStd
	_ = fastjson.ConfigFastest
	var _ fastjson.Config
	var _ fastjson.API
	var _ fastjson.Encoder
	var _ fastjson.Decoder
	var _ fastjson.NoCopyRawMessage

	if b, err := fastjson.Marshal(map[string]int{"a": 1}); err != nil || !json.Valid(b) {
		t.Fatalf("fastjson.Marshal: %v %s", err, b)
	}
	if s, err := fastjson.MarshalString(map[string]int{"a": 1}); err != nil || !json.Valid([]byte(s)) {
		t.Fatalf("fastjson.MarshalString: %v %q", err, s)
	}
	if b, err := fastjson.MarshalIndent(map[string]int{"a": 1}, "", "  "); err != nil || !json.Valid(b) {
		t.Fatalf("fastjson.MarshalIndent: %v %s", err, b)
	}
	var out map[string]int
	if err := fastjson.Unmarshal([]byte(`{"a":1}`), &out); err != nil {
		t.Fatalf("fastjson.Unmarshal: %v", err)
	}
	if err := fastjson.UnmarshalString(`{"a":1}`, &out); err != nil {
		t.Fatalf("fastjson.UnmarshalString: %v", err)
	}
	if !fastjson.Valid([]byte(`{"a":1}`)) {
		t.Fatalf("fastjson.Valid failed")
	}
	if !fastjson.ValidString(`{"a":1}`) {
		t.Fatalf("fastjson.ValidString failed")
	}
	if _, err := fastjson.Get([]byte(`{"a":1}`), "a"); err != nil {
		t.Fatalf("fastjson.Get: %v", err)
	}
	if _, err := fastjson.GetFromString(`{"a":1}`, "a"); err != nil {
		t.Fatalf("fastjson.GetFromString: %v", err)
	}
	if _, err := fastjson.GetCopyFromString(`{"a":1}`, "a"); err != nil {
		t.Fatalf("fastjson.GetCopyFromString: %v", err)
	}
	if _, err := fastjson.GetWithOptions([]byte(`{"a":1}`), ast.SearchOptions{}, "a"); err != nil {
		t.Fatalf("fastjson.GetWithOptions: %v", err)
	}
	var buf bytes.Buffer
	enc := fastjson.NewEncoder(&buf)
	if err := enc.Encode(map[string]int{"a": 1}); err != nil {
		t.Fatalf("fastjson.NewEncoder.Encode: %v", err)
	}
	dec := fastjson.NewDecoder(strings.NewReader(`{"a":1}`))
	var v map[string]int
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("fastjson.NewDecoder.Decode: %v", err)
	}
	if err := fastjson.Pretouch(reflect.TypeOf(map[string]int{})); err != nil {
		t.Fatalf("fastjson.Pretouch: %v", err)
	}
	if err := fastjson.PretouchMany([]reflect.Type{reflect.TypeOf(map[string]int{})}); err != nil {
		t.Fatalf("fastjson.PretouchMany: %v", err)
	}
}

// TestCompileCompatibilityStdjsonv2 exercises the stdjsonv2 surface. Under the
// default (non-jsonv2) build this is the disabled stub; under GOEXPERIMENT=jsonv2
// it is the real backend. We only require it compiles and the API surface is
// addressable.
func TestCompileCompatibilityStdjsonv2(t *testing.T) {
	_ = stdjsonv2.ConfigDefault
	_ = stdjsonv2.ConfigStd
	_ = stdjsonv2.ConfigFastest
	cfg := stdjsonv2.Config{UseNumber: true}
	api := cfg.Froze()
	_, _ = api.MarshalToString(map[string]int{"a": 1})
	_, _ = api.Marshal(map[string]int{"a": 1})
	_, _ = api.MarshalIndent(map[string]int{"a": 1}, "", "  ")
	var apiOut map[string]int
	_ = api.UnmarshalFromString(`{"a":1}`, &apiOut)
	_ = api.Unmarshal([]byte(`{"a":1}`), &apiOut)
	_ = api.Valid([]byte(`{"a":1}`))
	apiEnc := api.NewEncoder(&bytes.Buffer{})
	apiEnc.SetEscapeHTML(true)
	apiEnc.SetIndent("", "  ")
	_ = apiEnc.Encode(map[string]int{"a": 1})
	apiDec := api.NewDecoder(strings.NewReader(`{"a":1}`))
	apiDec.UseNumber()
	apiDec.DisallowUnknownFields()
	_ = apiDec.More()
	_ = apiDec.Buffered()
	_ = apiDec.Decode(&apiOut)

	// These functions exist in both build modes; under the stub they return
	// ErrJSONv2ExperimentDisabled, so we tolerate errors.
	b, err := stdjsonv2.Marshal(map[string]int{"a": 1})
	_ = b
	_ = err
	s, err := stdjsonv2.MarshalString(map[string]int{"a": 1})
	_ = s
	_ = err
	b2, err := stdjsonv2.MarshalIndent(map[string]int{"a": 1}, "", "  ")
	_ = b2
	_ = err
	var out map[string]int
	_ = stdjsonv2.Unmarshal([]byte(`{"a":1}`), &out)
	_ = stdjsonv2.UnmarshalString(`{"a":1}`, &out)
	_ = stdjsonv2.Valid([]byte(`{"a":1}`))
	_ = stdjsonv2.ValidString(`{"a":1}`)
	_, _ = stdjsonv2.Get([]byte(`{"a":1}`), "a")
	_, _ = stdjsonv2.GetFromString(`{"a":1}`, "a")
	_, _ = stdjsonv2.GetCopyFromString(`{"a":1}`, "a")
	_, _ = stdjsonv2.GetWithOptions([]byte(`{"a":1}`), ast.SearchOptions{}, "a")

	var buf bytes.Buffer
	enc := stdjsonv2.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]int{"a": 1})
	dec := stdjsonv2.NewDecoder(strings.NewReader(`{"a":1}`))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	_ = dec.More()
	_ = dec.Buffered()
	_ = dec.Decode(&out)
	_ = stdjsonv2.Pretouch(reflect.TypeOf(map[string]int{}))
	_ = stdjsonv2.PretouchMany([]reflect.Type{reflect.TypeOf(map[string]int{})})
}

// TestCompileCompatibilityUnquote exercises the unquote package surface.
func TestCompileCompatibilityUnquote(t *testing.T) {
	// Input is unquoted content (text between the surrounding quotes).
	if s, e := unquote.String(`hello`); e != 0 || s != "hello" {
		t.Fatalf("unquote.String plain: %q %v", s, e)
	}
	if s, e := unquote.String(`a\/b`); e != 0 || s != "a/b" {
		t.Fatalf("unquote.String escaped: %q %v", s, e)
	}
	var dst []byte
	if e := unquote.IntoBytes(`hello`, &dst); e != 0 || string(dst) != "hello" {
		t.Fatalf("unquote.IntoBytes: %q %v", dst, e)
	}
}

// TestCompileCompatibilityUTF8 exercises the utf8 package surface.
func TestCompileCompatibilityUTF8(t *testing.T) {
	if !utf8.Validate([]byte("hello")) {
		t.Fatalf("Validate failed")
	}
	if !utf8.ValidateString("hello") {
		t.Fatalf("ValidateString failed")
	}
	if got := utf8.CorrectWith(nil, []byte("hello"), "?"); !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("CorrectWith: %q", got)
	}
}

// TestCompileCompatibilityAST exercises the ast package surface.
func TestCompileCompatibilityAST(t *testing.T) {
	// Constants.
	_ = ast.V_NONE
	_ = ast.V_ERROR
	_ = ast.V_NULL
	_ = ast.V_TRUE
	_ = ast.V_FALSE
	_ = ast.V_ARRAY
	_ = ast.V_OBJECT
	_ = ast.V_STRING
	_ = ast.V_NUMBER
	_ = ast.V_ANY

	// Sentinels.
	_ = ast.ErrNotExist
	_ = ast.ErrUnsupportType
	_ = ast.VisitOPSkip

	// Constructors returning Node values.
	n := ast.NewNull()
	if n.Type() != ast.V_NULL {
		t.Fatalf("NewNull type: %v", n.Type())
	}
	b := ast.NewBool(true)
	if bt, _ := b.Bool(); bt != true {
		t.Fatalf("NewBool: %v", bt)
	}
	s := ast.NewString("hello")
	if got, _ := s.String(); got != "hello" {
		t.Fatalf("NewString: %v", got)
	}
	num := ast.NewNumber("123")
	if got, _ := num.Int64(); got != 123 {
		t.Fatalf("NewNumber: %v", got)
	}
	arr := ast.NewArray([]ast.Node{ast.NewBool(true), ast.NewBool(false)})
	if l, _ := arr.Len(); l != 2 {
		t.Fatalf("NewArray len: %v", l)
	}
	obj := ast.NewObject([]ast.Pair{{Key: "k", Value: ast.NewNumber("1")}})
	if l, _ := obj.Len(); l != 1 {
		t.Fatalf("NewObject len: %v", l)
	}
	p := ast.NewPair("k", ast.NewNull())
	_ = p
	// NewRaw returns a value; pointer-receiver methods need an addressable var.
	raw := ast.NewRaw(`{"x":1}`)
	if !raw.IsRaw() {
		t.Fatalf("NewRaw.IsRaw false")
	}
	if r, err := raw.Raw(); err != nil || r != `{"x":1}` {
		t.Fatalf("NewRaw.Raw: %q %v", r, err)
	}
	rawCR := ast.NewRawConcurrentRead(`{"x":1}`)
	if !rawCR.IsRaw() {
		t.Fatalf("NewRawConcurrentRead.IsRaw false")
	}
	bytes := ast.NewBytes([]byte(`"hi"`))
	// NewBytes returns a base64-encoded string node, not a raw node.
	if bytes.Type() != ast.V_STRING {
		t.Fatalf("NewBytes type: %v", bytes.Type())
	}
	if got, _ := bytes.String(); got == "" {
		t.Fatalf("NewBytes produced empty string")
	}
	any := ast.NewAny(map[string]int{"a": 1})
	_ = any

	// Node methods (pointer receiver mostly).
	if !n.Exists() {
		t.Fatalf("Exists false on NewNull")
	}
	if !n.Valid() {
		t.Fatalf("Valid false on NewNull")
	}
	if err := n.Check(); err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if err := n.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if err := n.LoadAll(); err != nil {
		t.Fatalf("LoadAll error: %v", err)
	}
	if got := n.TypeSafe(); got != ast.V_NULL {
		t.Fatalf("TypeSafe: %v", got)
	}
	if err := n.Error(); err != "" {
		t.Fatalf("Error: %v", err)
	}
	if _, err := b.StrictBool(); err != nil {
		t.Fatalf("StrictBool: %v", err)
	}
	if _, err := num.StrictInt64(); err != nil {
		t.Fatalf("StrictInt64: %v", err)
	}
	if _, err := num.StrictFloat64(); err != nil {
		t.Fatalf("StrictFloat64: %v", err)
	}
	if _, err := num.StrictNumber(); err != nil {
		t.Fatalf("StrictNumber: %v", err)
	}
	if _, err := s.StrictString(); err != nil {
		t.Fatalf("StrictString: %v", err)
	}
	if c, _ := arr.Cap(); c < 0 {
		t.Fatalf("Cap: %v", c)
	}

	// Get / Index / GetByPath on object/array.
	if got := obj.Get("k"); got == nil || !got.Exists() {
		t.Fatalf("Get(k) failed")
	}
	if got := arr.Index(0); got == nil || !got.Exists() {
		t.Fatalf("Index(0) failed")
	}
	if got := obj.GetByPath("k"); got == nil || !got.Exists() {
		t.Fatalf("GetByPath(k) failed")
	}
	if got := obj.IndexOrGet(0, "k"); got == nil {
		t.Fatalf("IndexOrGet failed")
	}
	if got, idx := obj.IndexOrGetWithIdx(0, "k"); got == nil && idx < 0 {
		t.Fatalf("IndexOrGetWithIdx failed")
	}
	if got := obj.IndexPair(0); got == nil {
		t.Fatalf("IndexPair failed")
	}

	// ForEach.
	if err := arr.ForEach(func(seq ast.Sequence, node *ast.Node) bool {
		_ = seq
		_ = node
		return true
	}); err != nil {
		t.Fatalf("ForEach: %v", err)
	}

	// Iterators.
	if it, err := arr.Values(); err != nil {
		t.Fatalf("Values: %v", err)
	} else {
		for it.HasNext() {
			var nv ast.Node
			if !it.Next(&nv) {
				break
			}
		}
		_ = it.Len()
		_ = it.Pos()
	}
	if it, err := obj.Properties(); err != nil {
		t.Fatalf("Properties: %v", err)
	} else {
		for it.HasNext() {
			var pr ast.Pair
			if !it.Next(&pr) {
				break
			}
		}
	}

	// Interface variants.
	if _, err := obj.Interface(); err != nil {
		t.Fatalf("Interface: %v", err)
	}
	if _, err := obj.InterfaceUseNumber(); err != nil {
		t.Fatalf("InterfaceUseNumber: %v", err)
	}
	if _, err := obj.InterfaceUseNode(); err != nil {
		t.Fatalf("InterfaceUseNode: %v", err)
	}
	if _, err := arr.Array(); err != nil {
		t.Fatalf("Array: %v", err)
	}
	if _, err := arr.ArrayUseNumber(); err != nil {
		t.Fatalf("ArrayUseNumber: %v", err)
	}
	if _, err := arr.ArrayUseNode(); err != nil {
		t.Fatalf("ArrayUseNode: %v", err)
	}
	if _, err := obj.Map(); err != nil {
		t.Fatalf("Map: %v", err)
	}
	if _, err := obj.MapUseNumber(); err != nil {
		t.Fatalf("MapUseNumber: %v", err)
	}
	if _, err := obj.MapUseNode(); err != nil {
		t.Fatalf("MapUseNode: %v", err)
	}

	// Mutators.
	if err := arr.Add(ast.NewNull()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := arr.AddAny(42); err != nil {
		t.Fatalf("AddAny: %v", err)
	}
	if ok, err := obj.Set("new", ast.NewNull()); err != nil || ok {
		// Sonic semantics: adding a new key reports false.
		t.Fatalf("Set: %v %v (Sonic returns false for a new key)", ok, err)
	}
	if ok, err := obj.SetAny("new2", 42); err != nil || !ok {
		// tolerate non-ok as long as no error
		_ = ok
		_ = err
	}
	if ok, err := arr.SetByIndex(0, ast.NewNull()); err != nil || !ok {
		_ = ok
		_ = err
	}
	if ok, err := arr.SetAnyByIndex(0, 42); err != nil || !ok {
		_ = ok
		_ = err
	}
	if ok, err := obj.Unset("new"); err != nil || !ok {
		_ = ok
		_ = err
	}
	if ok, err := arr.UnsetByIndex(0); err != nil || !ok {
		_ = ok
		_ = err
	}
	if err := arr.Pop(); err != nil {
		// tolerate errors if array becomes empty
		_ = err
	}
	if err := arr.Move(0, 1); err != nil {
		// may fail if out of range; tolerate
		_ = err
	}
	if err := obj.SortKeys(true); err != nil {
		t.Fatalf("SortKeys: %v", err)
	}

	// MarshalJSON / UnmarshalJSON.
	mb, err := obj.MarshalJSON()
	if err != nil || !json.Valid(mb) {
		t.Fatalf("MarshalJSON: %v %s", err, mb)
	}
	var target ast.Node
	if err := target.UnmarshalJSON([]byte(`{"a":1}`)); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Searcher.
	sr := ast.NewSearcher(`{"a":{"b":[1,2]}}`)
	node, err := sr.GetByPath("a", "b", 0)
	if err != nil {
		t.Fatalf("Searcher.GetByPath: %v", err)
	}
	if v, err := node.Int64(); err != nil || v != 1 {
		t.Fatalf("Searcher value: %v %v", v, err)
	}
	sr2 := ast.NewSearcher(`{"a":{"b":[1,2]}}`)
	node2, err := sr2.GetByPathCopy("a", "b", 1)
	if err != nil {
		t.Fatalf("Searcher.GetByPathCopy: %v", err)
	}
	if v, err := node2.Int64(); err != nil || v != 2 {
		t.Fatalf("Searcher copy value: %v %v", v, err)
	}

	// SearchOptions.
	so := ast.SearchOptions{ValidateJSON: true, CopyReturn: true, ConcurrentRead: true}
	_ = so

	// Parser.
	parser := ast.NewParser(`{"a":1}`)
	pnode, perr := parser.Parse()
	if perr != 0 {
		t.Fatalf("Parser.Parse: %v", parser.ExportError(perr))
	}
	if p := parser.Pos(); p < 0 {
		t.Fatalf("Parser.Pos: %v", p)
	}
	_ = pnode
	parserObj := ast.NewParserObj(`{"a":1}`)
	_, _ = parserObj.Parse()

	// SyntaxError.
	se := &ast.SyntaxError{Pos: 1, Src: "{", Code: 0, Msg: "bad"}
	_ = se.Error()

	// Loads.
	if _, _, err := ast.Loads(`{"a":1}`); err != nil {
		t.Fatalf("Loads: %v", err)
	}
	if _, _, err := ast.LoadsUseNumber(`{"a":1}`); err != nil {
		t.Fatalf("LoadsUseNumber: %v", err)
	}

	// Preorder + Visitor.
	var vis ast.Visitor = &compileVisitor{}
	if err := ast.Preorder(`{"a":1,"b":[true,null]}`, vis, &ast.VisitorOptions{OnlyNumber: false}); err != nil {
		t.Fatalf("Preorder: %v", err)
	}
}

// compileVisitor implements ast.Visitor to verify the interface signature.
type compileVisitor struct{}

func (c *compileVisitor) OnNull() error                        { return nil }
func (c *compileVisitor) OnBool(bool) error                    { return nil }
func (c *compileVisitor) OnString(string) error                { return nil }
func (c *compileVisitor) OnInt64(int64, json.Number) error     { return nil }
func (c *compileVisitor) OnFloat64(float64, json.Number) error { return nil }
func (c *compileVisitor) OnObjectBegin(capacity int) error     { return nil }
func (c *compileVisitor) OnObjectKey(key string) error         { return nil }
func (c *compileVisitor) OnObjectEnd() error                   { return nil }
func (c *compileVisitor) OnArrayBegin(capacity int) error      { return nil }
func (c *compileVisitor) OnArrayEnd() error                    { return nil }

// TestCompileCompatibilityTypes keeps io alive for stream decoders/encoders.
func TestCompileCompatibilityTypes(t *testing.T) {
	var _ io.Reader = strings.NewReader("")
	var _ io.Writer = &bytes.Buffer{}
}
