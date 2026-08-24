package sonic

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/bytedance/sonic/ast"
)

type apiSample struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type rootCountingMarshaler struct {
	calls int
}

func (m *rootCountingMarshaler) MarshalJSON() ([]byte, error) {
	m.calls++
	return []byte(`{"value":"<tag>"}`), nil
}

type rootSentinelErrorMarshaler struct {
	err error
}

func (m rootSentinelErrorMarshaler) MarshalJSON() ([]byte, error) {
	return nil, m.err
}

func TestRootMarshalEncodesOnceWithoutHTMLEscaping(t *testing.T) {
	value := &rootCountingMarshaler{}
	encoded, err := Marshal(value)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	if value.calls != 1 {
		t.Fatalf("MarshalJSON calls = %d, want 1", value.calls)
	}
	if !bytes.Contains(encoded, []byte(`<tag>`)) {
		t.Fatalf("Marshal output = %s, want literal <tag>", encoded)
	}
	if len(encoded) > 0 && encoded[len(encoded)-1] == '\n' {
		t.Fatalf("Marshal output has trailing newline: %q", encoded)
	}

	wantErr := errors.New("marshal boom")
	_, err = Marshal(rootSentinelErrorMarshaler{err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Marshal error = %v, want errors.Is(_, %v)", err, wantErr)
	}
}

func TestRootMarshalIndentEncodesOnceWithoutHTMLEscaping(t *testing.T) {
	value := &rootCountingMarshaler{}
	encoded, err := MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent error = %v", err)
	}
	if value.calls != 1 {
		t.Fatalf("MarshalJSON calls = %d, want 1", value.calls)
	}
	if !bytes.Contains(encoded, []byte(`<tag>`)) {
		t.Fatalf("MarshalIndent output = %s, want literal <tag>", encoded)
	}
	if bytes.HasSuffix(encoded, []byte{'\n'}) {
		t.Fatalf("MarshalIndent output has trailing newline: %q", encoded)
	}
}

func TestRootMarshalUnmarshalAndConfig(t *testing.T) {
	b, err := Marshal(apiSample{Name: "<x>", Count: 2})
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	if !Valid(b) {
		t.Fatalf("Marshal output is invalid JSON: %s", b)
	}
	var out apiSample
	if err := Unmarshal(b, &out); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if out != (apiSample{Name: "<x>", Count: 2}) {
		t.Fatalf("out = %+v", out)
	}
	api := Config{EscapeHTML: true, SortMapKeys: true}.Froze()
	encoded, err := api.Marshal(map[string]string{"x": "<tag>"})
	if err != nil {
		t.Fatalf("api.Marshal error = %v", err)
	}
	if !bytes.Contains(encoded, []byte(`\u003c`)) {
		t.Fatalf("EscapeHTML output = %s", encoded)
	}
}

func TestRootGetAndNoCopyRawMessage(t *testing.T) {
	n, err := Get([]byte(`{"a":[{"b":3}]}`), "a", 0, "b")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if got, err := n.Int64(); err != nil || got != 3 {
		t.Fatalf("Get value = %d, %v", got, err)
	}
	var raw NoCopyRawMessage
	src := []byte(`{"x":1}`)
	if err := raw.UnmarshalJSON(src); err != nil {
		t.Fatalf("NoCopyRawMessage.UnmarshalJSON error = %v", err)
	}
	if len(raw) != len(src) || &raw[0] != &src[0] {
		t.Fatalf("NoCopyRawMessage copied data")
	}
	marshaled, err := raw.MarshalJSON()
	if err != nil || string(marshaled) != string(src) {
		t.Fatalf("MarshalJSON = %s, %v", marshaled, err)
	}
}

func TestNoCopyRawMessageNilMarshalEncodesNull(t *testing.T) {
	var raw NoCopyRawMessage

	direct, err := raw.MarshalJSON()
	if err != nil {
		t.Fatalf("NoCopyRawMessage.MarshalJSON() error = %v", err)
	}
	if got := string(direct); got != "null" {
		t.Fatalf("NoCopyRawMessage.MarshalJSON() = %q, want null", got)
	}

	encoded, err := Marshal(raw)
	if err != nil {
		t.Fatalf("Marshal(nil NoCopyRawMessage) error = %v", err)
	}
	if got := string(encoded); got != "null" {
		t.Fatalf("Marshal(nil NoCopyRawMessage) = %q, want null", got)
	}
}

func TestRootGetEscapedObjectKey(t *testing.T) {
	for _, tt := range []struct {
		json string
		key  string
	}{
		{json: `{"\u0061":1}`, key: "a"},
		{json: `{"a\/b":1}`, key: "a/b"},
		{json: `{"a\"b":1}`, key: `a"b`},
	} {
		n, err := Get([]byte(tt.json), tt.key)
		if err != nil {
			t.Fatalf("Get(%s, %q) error = %v", tt.json, tt.key, err)
		}
		if got, err := n.Int64(); err != nil || got != 1 {
			t.Fatalf("Get(%s, %q) = %d, %v; want 1, nil", tt.json, tt.key, got, err)
		}
	}
}

func TestRootGetRejectsMalformedJSONAtRoot(t *testing.T) {
	if _, err := Get([]byte(`{"bad":`)); err == nil {
		t.Fatalf(`Get({"bad":) error = nil, want malformed JSON error`)
	}
}

func TestRootDecoderEncoderInterfaces(t *testing.T) {
	api := Config{UseNumber: true}.Froze()
	dec := api.NewDecoder(strings.NewReader(`{"n":123}`))
	dec.UseNumber()
	var out map[string]interface{}
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if _, ok := out["n"].(json.Number); !ok {
		t.Fatalf("n type = %T, want json.Number", out["n"])
	}
	var buf bytes.Buffer
	enc := api.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]string{"x": "y"}); err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if !Valid(buf.Bytes()) {
		t.Fatalf("encoded stream invalid: %s", buf.String())
	}
}

func TestConfigUseInt64UnmarshalConvertsNestedInterfaceValues(t *testing.T) {
	api := Config{UseInt64: true}.Froze()
	var out map[string]interface{}
	if err := api.Unmarshal([]byte(`{"n":1,"a":[2,{"b":3}],"f":1.5,"big":99999999999999999999}`), &out); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if got, ok := out["n"].(int64); !ok || got != 1 {
		t.Fatalf("n = %v (%T), want int64(1)", out["n"], out["n"])
	}
	a := out["a"].([]interface{})
	if got, ok := a[0].(int64); !ok || got != 2 {
		t.Fatalf("a[0] = %v (%T), want int64(2)", a[0], a[0])
	}
	b := a[1].(map[string]interface{})["b"]
	if got, ok := b.(int64); !ok || got != 3 {
		t.Fatalf("a[1].b = %v (%T), want int64(3)", b, b)
	}
	if _, ok := out["f"].(json.Number); !ok {
		t.Fatalf("f = %v (%T), want json.Number", out["f"], out["f"])
	}
	if _, ok := out["big"].(json.Number); !ok {
		t.Fatalf("big = %v (%T), want json.Number", out["big"], out["big"])
	}
}

func TestConfigUseInt64StreamDecoderConvertsNestedInterfaceValues(t *testing.T) {
	dec := Config{UseInt64: true}.Froze().NewDecoder(strings.NewReader(`{"n":1,"a":[2]}`))
	var out map[string]interface{}
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if got, ok := out["n"].(int64); !ok || got != 1 {
		t.Fatalf("n = %v (%T), want int64(1)", out["n"], out["n"])
	}
	if got, ok := out["a"].([]interface{})[0].(int64); !ok || got != 2 {
		t.Fatalf("a[0] = %v (%T), want int64(2)", out["a"], out["a"])
	}
}

func TestConfigUseNumberTakesPrecedenceOverUseInt64(t *testing.T) {
	api := Config{UseNumber: true, UseInt64: true}.Froze()

	var scalar interface{}
	if err := api.Unmarshal([]byte(`1`), &scalar); err != nil {
		t.Fatalf("Unmarshal scalar error = %v", err)
	}
	if got, ok := scalar.(json.Number); !ok || got.String() != "1" {
		t.Fatalf("scalar = %v (%T), want json.Number(1)", scalar, scalar)
	}

	var nested map[string]interface{}
	if err := api.Unmarshal([]byte(`{"n":1,"a":[2]}`), &nested); err != nil {
		t.Fatalf("Unmarshal nested error = %v", err)
	}
	array, ok := nested["a"].([]interface{})
	if !ok || len(array) != 1 {
		t.Fatalf("a = %v (%T), want one-element []interface{}", nested["a"], nested["a"])
	}
	if got, ok := nested["n"].(json.Number); !ok || got.String() != "1" {
		t.Fatalf("nested.n = %v (%T), want json.Number(1)", nested["n"], nested["n"])
	}
	if got, ok := array[0].(json.Number); !ok || got.String() != "2" {
		t.Fatalf("nested.a[0] = %v (%T), want json.Number(2)", array[0], array[0])
	}

	dec := api.NewDecoder(strings.NewReader(`1`))
	var streamed interface{}
	if err := dec.Decode(&streamed); err != nil {
		t.Fatalf("stream Decode error = %v", err)
	}
	if got, ok := streamed.(json.Number); !ok || got.String() != "1" {
		t.Fatalf("streamed = %v (%T), want json.Number(1)", streamed, streamed)
	}
}

func TestConfigUnmarshalRejectsTrailingGarbageInDecoderBackedPaths(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		val  interface{}
	}{
		{name: "UseNumber", cfg: Config{UseNumber: true}, val: &map[string]interface{}{}},
		{name: "UseInt64", cfg: Config{UseInt64: true}, val: &map[string]interface{}{}},
		{name: "DisallowUnknownFields", cfg: Config{DisallowUnknownFields: true}, val: &apiSample{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Froze().Unmarshal([]byte(`{"name":"ok","count":1}x`), tt.val); err == nil {
				t.Fatalf("Unmarshal accepted trailing garbage")
			}
		})
	}
}

func TestConfigUnmarshalAllowsTrailingWhitespaceInDecoderBackedPaths(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		val  interface{}
	}{
		{name: "UseNumber", cfg: Config{UseNumber: true}, val: &map[string]interface{}{}},
		{name: "UseInt64", cfg: Config{UseInt64: true}, val: &map[string]interface{}{}},
		{name: "DisallowUnknownFields", cfg: Config{DisallowUnknownFields: true}, val: &apiSample{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Froze().Unmarshal([]byte("{\"name\":\"ok\",\"count\":1}\n \t"), tt.val); err != nil {
				t.Fatalf("Unmarshal with trailing whitespace error = %v", err)
			}
		})
	}
}

func TestConfigNoEncoderNewlineSuppressesStreamTrailingNewline(t *testing.T) {
	var buf bytes.Buffer
	enc := Config{NoEncoderNewline: true}.Froze().NewEncoder(&buf)
	if err := enc.Encode("x"); err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if got := buf.String(); got != `"x"` {
		t.Fatalf("encoded = %q, want %q", got, `"x"`)
	}

	buf.Reset()
	enc = Config{}.Froze().NewEncoder(&buf)
	if err := enc.Encode("x"); err != nil {
		t.Fatalf("default Encode error = %v", err)
	}
	if got := buf.String(); got != "\"x\"\n" {
		t.Fatalf("default encoded = %q, want quoted x plus newline", got)
	}
}

func TestRootStreamEncoderCompletesShortWrites(t *testing.T) {
	w := &rootOneByteWriter{}
	enc := Config{}.Froze().NewEncoder(w)
	if err := enc.Encode(map[string]int{"a": 1}); err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if got := w.String(); got != "{\"a\":1}\n" {
		t.Fatalf("encoded = %q, want %q", got, "{\"a\":1}\n")
	}
}

func TestRootStreamEncoderRejectsZeroProgress(t *testing.T) {
	enc := Config{}.Froze().NewEncoder(rootZeroProgressWriter{})
	if err := enc.Encode(map[string]int{"a": 1}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Encode error = %v, want io.ErrShortWrite", err)
	}
}

func TestRootStreamEncoderPropagatesWriterError(t *testing.T) {
	want := errors.New("write boom")
	enc := Config{}.Froze().NewEncoder(rootErrWriter{err: want})
	if err := enc.Encode(map[string]int{"a": 1}); err != want {
		t.Fatalf("Encode error = %v, want original error %v", err, want)
	}
}

type rootOneByteWriter struct {
	bytes.Buffer
}

func (w *rootOneByteWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return w.Buffer.Write(p[:1])
}

type rootZeroProgressWriter struct{}

func (rootZeroProgressWriter) Write([]byte) (int, error) {
	return 0, nil
}

type rootErrWriter struct {
	err error
}

func (w rootErrWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestRootCompileCompatibility(t *testing.T) {
	var _ API = ConfigDefault
	var _ ast.Node
	if err := Pretouch(reflect.TypeOf(apiSample{})); err != nil {
		t.Fatalf("Pretouch error = %v", err)
	}
}
