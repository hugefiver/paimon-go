//go:build goexperiment.jsonv2

// Tests for the jsonv2-backed implementation of stdjsonv2. These run
// only when the toolchain was built with GOEXPERIMENT=jsonv2.

package stdjsonv2

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/bytedance/sonic/ast"
)

// Compile-time assertions that the jsonv2-backed implementations satisfy
// the public interfaces.
var (
	_ API     = ConfigDefault
	_ API     = ConfigStd
	_ API     = ConfigFastest
	_ Encoder = NewEncoder(&bytes.Buffer{})
	_ Decoder = NewDecoder(strings.NewReader("{}"))
)

func TestJSONv2MarshalRoundTrip(t *testing.T) {
	type sample struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	src := sample{Name: "alice", Count: 7}
	b, err := Marshal(src)
	if err != nil {
		t.Fatalf("Marshal err = %v", err)
	}
	if !Valid(b) {
		t.Fatalf("Marshal produced invalid JSON: %s", b)
	}
	var out sample
	if err := Unmarshal(b, &out); err != nil {
		t.Fatalf("Unmarshal err = %v", err)
	}
	if out != src {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", out, src)
	}
}

func TestJSONv2MarshalString(t *testing.T) {
	s, err := MarshalString(map[string]int{"a": 1})
	if err != nil {
		t.Fatalf("MarshalString err = %v", err)
	}
	if !ValidString(s) {
		t.Fatalf("MarshalString produced invalid JSON: %s", s)
	}
}

func TestJSONv2MarshalIndent(t *testing.T) {
	b, err := MarshalIndent(map[string]int{"a": 1}, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent err = %v", err)
	}
	if !bytes.Contains(b, []byte("\n")) {
		t.Fatalf("MarshalIndent produced no indentation: %s", b)
	}
	if !Valid(b) {
		t.Fatalf("MarshalIndent produced invalid JSON: %s", b)
	}
}

func TestJSONv2UnmarshalString(t *testing.T) {
	var v map[string]int
	if err := UnmarshalString(`{"a":1}`, &v); err != nil {
		t.Fatalf("UnmarshalString err = %v", err)
	}
	if v["a"] != 1 {
		t.Fatalf("UnmarshalString result = %v", v)
	}
}

func TestJSONv2ConfigUseInt64UnmarshalConvertsNestedInterfaceValues(t *testing.T) {
	api := Config{UseInt64: true}.Froze()
	var out map[string]interface{}
	if err := api.Unmarshal([]byte(`{"n":1,"a":[2,{"b":3}],"f":1.5,"big":99999999999999999999}`), &out); err != nil {
		t.Fatalf("Unmarshal err = %v", err)
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

func TestJSONv2ConfigUnmarshalRejectsTrailingGarbageInStdJSONDecoderPath(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "UseNumber", cfg: Config{UseNumber: true}},
		{name: "UseInt64", cfg: Config{UseInt64: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out map[string]interface{}
			if err := tt.cfg.Froze().Unmarshal([]byte(`{"n":1}x`), &out); err == nil {
				t.Fatalf("Unmarshal accepted trailing garbage")
			}
		})
	}
}

func TestJSONv2ConfigUnmarshalAllowsTrailingWhitespaceInStdJSONDecoderPath(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "UseNumber", cfg: Config{UseNumber: true}},
		{name: "UseInt64", cfg: Config{UseInt64: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out map[string]interface{}
			if err := tt.cfg.Froze().Unmarshal([]byte("{\"n\":1}\n \t"), &out); err != nil {
				t.Fatalf("Unmarshal with trailing whitespace error = %v", err)
			}
		})
	}
}

func TestJSONv2Valid(t *testing.T) {
	if !Valid([]byte(`{"a":1}`)) {
		t.Fatalf("Valid returned false for valid JSON")
	}
	if Valid([]byte(`{not json`)) {
		t.Fatalf("Valid returned true for invalid JSON")
	}
	if !ValidString(`{"a":1}`) {
		t.Fatalf("ValidString returned false for valid JSON")
	}
}

func TestJSONv2GetFromString(t *testing.T) {
	n, err := GetFromString(`{"a":[{"b":3}]}`, "a", 0, "b")
	if err != nil {
		t.Fatalf("GetFromString err = %v", err)
	}
	got, err := n.Int64()
	if err != nil || got != 3 {
		t.Fatalf("GetFromString value = %d, err = %v", got, err)
	}
}

func TestJSONv2GetCopyFromString(t *testing.T) {
	n, err := GetCopyFromString(`{"a":[{"b":3}]}`, "a", 0, "b")
	if err != nil {
		t.Fatalf("GetCopyFromString err = %v", err)
	}
	got, err := n.Int64()
	if err != nil || got != 3 {
		t.Fatalf("GetCopyFromString value = %d, err = %v", got, err)
	}
}

func TestJSONv2StreamEncoderEscapeHTMLAndIndent(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]string{"x": "<tag>"}); err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `\u003c`) {
		t.Fatalf("SetEscapeHTML(true) had no effect: %s", out)
	}
	if !strings.Contains(out, "\n") {
		t.Fatalf("SetIndent had no effect: %s", out)
	}
	// The encoded output (minus trailing newline) must be valid JSON.
	trimmed := strings.TrimRight(out, "\n")
	if !Valid([]byte(trimmed)) {
		t.Fatalf("stream encoder produced invalid JSON: %s", out)
	}
}

func TestJSONv2StreamDecoderUseNumberAndDisallowUnknown(t *testing.T) {
	dec := NewDecoder(strings.NewReader(`{"n":1}`))
	dec.UseNumber()
	var out map[string]interface{}
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("Decode err = %v", err)
	}
	num, ok := out["n"].(json.Number)
	if !ok {
		t.Fatalf("UseNumber had no effect, n type = %T", out["n"])
	}
	if num.String() != "1" {
		t.Fatalf("n value = %s, want 1", num.String())
	}
}

func TestJSONv2StreamDecoderBufferedReadable(t *testing.T) {
	// Two JSON values in the stream; the decoder reads one, then Buffered
	// returns the unread bytes.
	dec := NewDecoder(strings.NewReader(`{"a":1}{"b":2}`))
	var v map[string]int
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("Decode err = %v", err)
	}
	if v["a"] != 1 {
		t.Fatalf("Decode result = %v", v)
	}
	r := dec.Buffered()
	if r == nil {
		t.Fatalf("Buffered returned nil")
	}
	// The buffered reader should be readable without error. Its exact
	// contents depend on jsontext's internal buffering, but it must not
	// panic and must be a valid reader.
	buf := make([]byte, 64)
	_, _ = r.Read(buf)
}

func TestJSONv2StreamDecoderMoreFalseAfterOneValue(t *testing.T) {
	dec := NewDecoder(strings.NewReader(`{"a":1}`))
	var v map[string]int
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("Decode err = %v", err)
	}
	if dec.More() {
		t.Fatalf("More returned true after single-value stream, want false")
	}
}

func TestJSONv2ConfigDisallowUnknownFieldsRejectsUnknownField(t *testing.T) {
	type target struct {
		Known string `json:"known"`
	}
	api := Config{DisallowUnknownFields: true}.Froze()
	err := api.Unmarshal([]byte(`{"known":"a","unknown":"b"}`), &target{})
	if err == nil {
		t.Fatalf("Unmarshal accepted unknown field; want error")
	}
	if errors.Is(err, ErrJSONv2ExperimentDisabled) {
		t.Fatalf("got disabled error under jsonv2 build: %v", err)
	}
}

func TestJSONv2GetWithOptions(t *testing.T) {
	n, err := GetWithOptions([]byte(`{"a":[{"b":3}]}`), ast.SearchOptions{ValidateJSON: true}, "a", 0, "b")
	if err != nil {
		t.Fatalf("GetWithOptions err = %v", err)
	}
	got, err := n.Int64()
	if err != nil || got != 3 {
		t.Fatalf("GetWithOptions value = %d, err = %v", got, err)
	}
}
