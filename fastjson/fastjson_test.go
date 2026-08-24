package fastjson

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	sonic "github.com/bytedance/sonic"
	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/option"
)

// Compile-time assertions that the wrapper types are aliases of the root
// package types. These guarantee that values flow between packages without
// conversion.
var (
	_ Config           = Config{}
	_ API              = ConfigDefault
	_ Encoder          = Encoder(nil)
	_ Decoder          = Decoder(nil)
	_ NoCopyRawMessage = NoCopyRawMessage(nil)
)

func TestPackageFunctionsAndConstructorsShareConfigDefault(t *testing.T) {
	oldConfigDefault := ConfigDefault
	defer func() { ConfigDefault = oldConfigDefault }()
	ConfigDefault = sonic.Config{EscapeHTML: true, UseNumber: true}.Froze()

	marshaled, err := Marshal("<")
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	if !bytes.Contains(marshaled, []byte(`\u003c`)) {
		t.Fatalf("Marshal did not use ConfigDefault EscapeHTML: %s", marshaled)
	}

	var encoderOutput bytes.Buffer
	if err := NewEncoder(&encoderOutput).Encode("<"); err != nil {
		t.Fatalf("NewEncoder.Encode error = %v", err)
	}
	if !bytes.Contains(encoderOutput.Bytes(), []byte(`\u003c`)) {
		t.Fatalf("NewEncoder did not use ConfigDefault EscapeHTML: %s", encoderOutput.String())
	}

	var unmarshaled interface{}
	if err := Unmarshal([]byte(`1`), &unmarshaled); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if _, ok := unmarshaled.(json.Number); !ok {
		t.Fatalf("Unmarshal did not use ConfigDefault UseNumber: got %T", unmarshaled)
	}

	var decoded interface{}
	if err := NewDecoder(strings.NewReader(`1`)).Decode(&decoded); err != nil {
		t.Fatalf("NewDecoder.Decode error = %v", err)
	}
	if _, ok := decoded.(json.Number); !ok {
		t.Fatalf("NewDecoder did not use ConfigDefault UseNumber: got %T", decoded)
	}

	marshaledString, err := MarshalString("<")
	if err != nil {
		t.Fatalf("MarshalString error = %v", err)
	}
	if !strings.Contains(marshaledString, `\u003c`) {
		t.Fatalf("MarshalString did not use ConfigDefault EscapeHTML: %s", marshaledString)
	}

	marshaledIndent, err := MarshalIndent(map[string]string{"x": "<"}, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent error = %v", err)
	}
	if !bytes.Contains(marshaledIndent, []byte("\n  ")) || !bytes.Contains(marshaledIndent, []byte(`\u003c`)) {
		t.Fatalf("MarshalIndent did not indent and HTML-escape with ConfigDefault: %s", marshaledIndent)
	}

	var unmarshaledString interface{}
	if err := UnmarshalString(`1`, &unmarshaledString); err != nil {
		t.Fatalf("UnmarshalString error = %v", err)
	}
	if _, ok := unmarshaledString.(json.Number); !ok {
		t.Fatalf("UnmarshalString did not use ConfigDefault UseNumber: got %T", unmarshaledString)
	}

	if !Valid([]byte(`{"x":"\u003c"}`)) || Valid([]byte(`{"x":`)) {
		t.Fatalf("Valid did not distinguish valid and invalid JSON")
	}
	if !ValidString(`{"x":"\u003c"}`) || ValidString(`{"x":`) {
		t.Fatalf("ValidString did not distinguish valid and invalid JSON")
	}
}

func TestWrappersMarshalUnmarshalAndValid(t *testing.T) {
	type sample struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	src := sample{Name: "<x>", Count: 2}

	b, err := Marshal(src)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	if !Valid(b) {
		t.Fatalf("Marshal output invalid JSON: %s", b)
	}
	if !ValidString(string(b)) {
		t.Fatalf("ValidString returned false for valid JSON")
	}

	var out sample
	if err := Unmarshal(b, &out); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if out != src {
		t.Fatalf("Unmarshal round-trip mismatch: got %+v, want %+v", out, src)
	}

	s, err := MarshalString(src)
	if err != nil {
		t.Fatalf("MarshalString error = %v", err)
	}
	if s != string(b) {
		t.Fatalf("MarshalString mismatch: got %q, want %q", s, string(b))
	}

	var out2 sample
	if err := UnmarshalString(s, &out2); err != nil {
		t.Fatalf("UnmarshalString error = %v", err)
	}
	if out2 != src {
		t.Fatalf("UnmarshalString mismatch: got %+v", out2)
	}
}

func TestWrappersMarshalIndent(t *testing.T) {
	b, err := MarshalIndent(map[string]int{"a": 1}, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent error = %v", err)
	}
	if !bytes.Contains(b, []byte("\n  ")) {
		t.Fatalf("MarshalIndent did not indent output: %s", b)
	}
}

func TestWrappersConfigDefaultImplementsAPI(t *testing.T) {
	// ConfigDefault is an API; ensure every API method is callable.
	var _ API = ConfigDefault

	b, err := ConfigDefault.Marshal(map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("ConfigDefault.Marshal error = %v", err)
	}
	if !Valid(b) {
		t.Fatalf("ConfigDefault.Marshal produced invalid JSON: %s", b)
	}

	s, err := ConfigDefault.MarshalToString(map[string]string{"k": "v"})
	if err != nil || !Valid([]byte(s)) {
		t.Fatalf("MarshalToString = %q, err = %v", s, err)
	}

	bi, err := ConfigDefault.MarshalIndent(map[string]string{"k": "v"}, "", "  ")
	if err != nil || !Valid(bi) {
		t.Fatalf("MarshalIndent error = %v", b)
	}

	var m map[string]string
	if err := ConfigDefault.Unmarshal([]byte(`{"k":"v"}`), &m); err != nil {
		t.Fatalf("ConfigDefault.Unmarshal error = %v", err)
	}
	if m["k"] != "v" {
		t.Fatalf("Unmarshal result = %v", m)
	}

	if err := ConfigDefault.UnmarshalFromString(`{"k":"v"}`, &m); err != nil {
		t.Fatalf("ConfigDefault.UnmarshalFromString error = %v", err)
	}

	var buf bytes.Buffer
	enc := ConfigDefault.NewEncoder(&buf)
	if err := enc.Encode(map[string]string{"k": "v"}); err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if !Valid(bytes.TrimRight(buf.Bytes(), "\n")) {
		t.Fatalf("stream encoder produced invalid JSON: %s", buf.String())
	}

	dec := ConfigDefault.NewDecoder(strings.NewReader(`{"k":"v"}`))
	var m2 map[string]string
	if err := dec.Decode(&m2); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if m2["k"] != "v" {
		t.Fatalf("Decode result = %v", m2)
	}

	if !ConfigDefault.Valid([]byte(`{"k":"v"}`)) {
		t.Fatalf("Valid returned false for valid JSON")
	}
}

func TestWrappersPreConfiguredInstances(t *testing.T) {
	// ConfigStd should HTML-escape by default.
	b, err := ConfigStd.Marshal(map[string]string{"x": "<tag>"})
	if err != nil {
		t.Fatalf("ConfigStd.Marshal error = %v", err)
	}
	if !bytes.Contains(b, []byte(`\u003c`)) {
		t.Fatalf("ConfigStd did not HTML-escape: %s", b)
	}

	// ConfigFastest should still produce valid JSON.
	b2, err := ConfigFastest.Marshal(map[string]int{"a": 1})
	if err != nil {
		t.Fatalf("ConfigFastest.Marshal error = %v", err)
	}
	if !Valid(b2) {
		t.Fatalf("ConfigFastest produced invalid JSON: %s", b2)
	}
}

func TestWrappersGetPath(t *testing.T) {
	const src = `{"a":[{"b":3}]}`
	n, err := Get([]byte(src), "a", 0, "b")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	got, err := n.Int64()
	if err != nil || got != 3 {
		t.Fatalf("Get value = %d, err = %v", got, err)
	}

	n2, err := GetFromString(src, "a", 0, "b")
	if err != nil {
		t.Fatalf("GetFromString error = %v", err)
	}
	got2, _ := n2.Int64()
	if got2 != 3 {
		t.Fatalf("GetFromString value = %d", got2)
	}

	n3, err := GetCopyFromString(src, "a", 0, "b")
	if err != nil {
		t.Fatalf("GetCopyFromString error = %v", err)
	}
	got3, _ := n3.Int64()
	if got3 != 3 {
		t.Fatalf("GetCopyFromString value = %d", got3)
	}

	n4, err := GetWithOptions([]byte(src), ast.SearchOptions{ValidateJSON: true}, "a", 0, "b")
	if err != nil {
		t.Fatalf("GetWithOptions error = %v", err)
	}
	got4, _ := n4.Int64()
	if got4 != 3 {
		t.Fatalf("GetWithOptions value = %d", got4)
	}
}

func TestWrappersNoCopyRawMessageAlias(t *testing.T) {
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
		t.Fatalf("MarshalJSON = %s, err = %v", marshaled, err)
	}
}

func TestWrappersNewEncoderAndNewDecoder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(map[string]string{"k": "v"}); err != nil {
		t.Fatalf("NewEncoder.Encode error = %v", err)
	}
	if !Valid(bytes.TrimRight(buf.Bytes(), "\n")) {
		t.Fatalf("NewEncoder produced invalid JSON: %s", buf.String())
	}
	enc.SetEscapeHTML(true)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]string{"x": "<tag>"}); err != nil {
		t.Fatalf("NewEncoder.Encode(2) error = %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`\u003c`)) {
		t.Fatalf("SetEscapeHTML(true) had no effect: %s", buf.String())
	}

	dec := NewDecoder(strings.NewReader(`{"n":1}`))
	dec.UseNumber()
	var out map[string]interface{}
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("NewDecoder.Decode error = %v", err)
	}
	if _, ok := out["n"].(json.Number); !ok {
		t.Fatalf("UseNumber had no effect, n type = %T", out["n"])
	}
	dec.DisallowUnknownFields()
	if dec.More() {
		t.Fatalf("More should be false after single-value stream")
	}
}

func TestWrappersPretouch(t *testing.T) {
	if err := Pretouch(reflect.TypeOf(struct{ X int }{})); err != nil {
		t.Fatalf("Pretouch error = %v", err)
	}
	if err := PretouchMany([]reflect.Type{reflect.TypeOf(struct{ X int }{})}, option.WithCompileMaxInlineDepth(2)); err != nil {
		t.Fatalf("PretouchMany error = %v", err)
	}
}
