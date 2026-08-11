package sonic

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/bytedance/sonic/ast"
)

type apiSample struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
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

func TestRootAcceptsRawControlByteInStringLikeSonic(t *testing.T) {
	data := []byte{'{', '"', 0x11, 'x', '"', ':', '1', '}'}
	if !Valid(data) {
		t.Fatalf("Valid(%q) = false, want true for Sonic-compatible raw control string", data)
	}
	var out map[string]interface{}
	if err := Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v, want nil", data, err)
	}
	if _, ok := out[string([]byte{0x11, 'x'})]; !ok {
		t.Fatalf("decoded keys = %#v, want raw-control key", out)
	}
}

func TestRootGetRejectsMalformedJSONAtRoot(t *testing.T) {
	if _, err := Get([]byte(`{"bad":`)); err == nil {
		t.Fatalf(`Get({"bad":) error = nil, want malformed JSON error`)
	}
}

func TestRootGetReturnsFirstValueBeforeTrailingGarbageLikeSonic(t *testing.T) {
	n, err := Get([]byte(`[1,true]x"",`))
	if err != nil {
		t.Fatalf("Get with trailing garbage error = %v, want nil", err)
	}
	raw, err := n.Raw()
	if err != nil {
		t.Fatalf("Raw() error = %v", err)
	}
	if raw != `[1,true]` {
		t.Fatalf("Raw() = %q, want first JSON value", raw)
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

func TestRootCompileCompatibility(t *testing.T) {
	var _ API = ConfigDefault
	var _ ast.Node
	if err := Pretouch(reflect.TypeOf(apiSample{})); err != nil {
		t.Fatalf("Pretouch error = %v", err)
	}
}
