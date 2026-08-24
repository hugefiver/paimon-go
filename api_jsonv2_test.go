//go:build sonic_jsonv2 && !sonic_stdjson && goexperiment.jsonv2

package sonic

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/internal/backend"
	"github.com/bytedance/sonic/stdjsonv2"
)

func TestBackendConfigToStdJSONV2ExactMapping(t *testing.T) {
	fields := []string{
		"EscapeHTML",
		"SortMapKeys",
		"CompactMarshaler",
		"NoQuoteTextMarshaler",
		"NoNullSliceOrMap",
		"UseInt64",
		"UseNumber",
		"UseUnicodeErrors",
		"DisallowUnknownFields",
		"CopyString",
		"ValidateString",
		"NoValidateJSONMarshaler",
		"NoValidateJSONSkip",
		"NoEncoderNewline",
		"EncodeNullForInfOrNan",
		"CaseSensitive",
	}

	backendType := reflect.TypeOf(backend.Config{})
	stdjsonv2Type := reflect.TypeOf(stdjsonv2.Config{})
	if backendType.NumField() != len(fields) || stdjsonv2Type.NumField() != len(fields) {
		t.Fatalf("config field counts = %d and %d, want %d", backendType.NumField(), stdjsonv2Type.NumField(), len(fields))
	}
	for i, name := range fields {
		if got := backendType.Field(i).Name; got != name {
			t.Fatalf("backend field %d = %q, want %q", i, got, name)
		}
		if got := stdjsonv2Type.Field(i).Name; got != name {
			t.Fatalf("stdjsonv2 field %d = %q, want %q", i, got, name)
		}
	}

	for _, active := range fields {
		var cfg backend.Config
		reflect.ValueOf(&cfg).Elem().FieldByName(active).SetBool(true)
		got := reflect.ValueOf(toStdJSONV2Config(cfg))
		for _, name := range fields {
			if want := name == active; got.FieldByName(name).Bool() != want {
				t.Fatalf("%s with %s set = %t, want %t", name, active, got.FieldByName(name).Bool(), want)
			}
		}
	}
}

func TestJSONV2BackendBindsFrozenConfig(t *testing.T) {
	cfg := Config{EscapeHTML: true}
	frozen := cfg.Froze()
	cfg.EscapeHTML = false

	got, err := frozen.Marshal(map[string]string{"x": "<tag>"})
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	if !bytes.Contains(got, []byte(`\u003c`)) {
		t.Fatalf("Marshal = %q, want HTML-escaped output", got)
	}
}

func TestJSONV2BuildTagRoutesRootAPI(t *testing.T) {
	rawControl := []byte{'{', '"', 0x11, 'x', '"', ':', '1', '}'}
	if Valid(rawControl) {
		t.Fatal("Valid accepted a raw control byte under sonic_jsonv2")
	}
	var out map[string]interface{}
	if err := Unmarshal(rawControl, &out); err == nil {
		t.Fatal("Unmarshal accepted a raw control byte under sonic_jsonv2")
	}
}

func TestJSONV2BuildTagRoutesRootGetSelectors(t *testing.T) {
	const invalid = `[1] trailing`
	tests := map[string]func() error{
		"Get":               func() error { _, err := Get([]byte(invalid)); return err },
		"GetFromString":     func() error { _, err := GetFromString(invalid); return err },
		"GetCopyFromString": func() error { _, err := GetCopyFromString(invalid); return err },
		"GetWithOptions": func() error {
			_, err := GetWithOptions([]byte(invalid), ast.SearchOptions{ValidateJSON: true})
			return err
		},
	}
	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatalf("%s accepted trailing data under sonic_jsonv2", name)
			}
		})
	}
}

func TestJSONV2RootMarshalIndentAndMappedConfig(t *testing.T) {
	cfg := Config{
		EscapeHTML:            true,
		SortMapKeys:           true,
		NoNullSliceOrMap:      true,
		DisallowUnknownFields: true,
		CaseSensitive:         true,
	}
	api := cfg.Froze()

	got, err := api.Marshal(map[string]string{"z": "last", "a": "<tag>"})
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	if want := `{"a":"\u003ctag\u003e","z":"last"}`; string(got) != want {
		t.Fatalf("Marshal = %q, want %q", got, want)
	}

	type nilCollections struct {
		Slice []string       `json:"slice"`
		Map   map[string]int `json:"map"`
	}
	got, err = api.Marshal(nilCollections{})
	if err != nil {
		t.Fatalf("Marshal nil collections error = %v", err)
	}
	if want := `{"slice":[],"map":{}}`; string(got) != want {
		t.Fatalf("Marshal nil collections = %q, want %q", got, want)
	}
	got, err = ConfigDefault.Marshal(nilCollections{})
	if err != nil {
		t.Fatalf("default Marshal nil collections error = %v", err)
	}
	if want := `{"slice":null,"map":null}`; string(got) != want {
		t.Fatalf("default Marshal nil collections = %q, want %q", got, want)
	}

	type target struct {
		Name string `json:"name"`
	}
	for name, src := range map[string]string{
		"case mismatch": `{"NAME":"value"}`,
		"unknown":       `{"name":"value","extra":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			var target target
			if err := api.Unmarshal([]byte(src), &target); err == nil {
				t.Fatalf("Unmarshal(%s) succeeded, want rejection", src)
			}
		})
	}

	got, err = api.MarshalIndent(map[string]int{"z": 2, "a": 1}, "PFX", "IX")
	if err != nil {
		t.Fatalf("MarshalIndent error = %v", err)
	}
	if want := "{\nPFXIX\"a\": 1,\nPFXIX\"z\": 2\nPFX}"; string(got) != want {
		t.Fatalf("MarshalIndent = %q, want %q", got, want)
	}
}

func TestJSONV2RootNumberModesAndAPIKind(t *testing.T) {
	if APIKind != UseSonicJSON {
		t.Fatalf("APIKind = %d, want UseSonicJSON (%d)", APIKind, UseSonicJSON)
	}

	var int64Out map[string]interface{}
	if err := (Config{UseInt64: true}).Froze().Unmarshal([]byte(`{"n":1,"a":[2]}`), &int64Out); err != nil {
		t.Fatalf("UseInt64 Unmarshal error = %v", err)
	}
	if got, ok := int64Out["n"].(int64); !ok || got != 1 {
		t.Fatalf("UseInt64 n = %v (%T), want int64(1)", int64Out["n"], int64Out["n"])
	}
	if got, ok := int64Out["a"].([]interface{})[0].(int64); !ok || got != 2 {
		t.Fatalf("UseInt64 a[0] = %v (%T), want int64(2)", int64Out["a"], int64Out["a"])
	}

	var numberOut map[string]interface{}
	if err := (Config{UseNumber: true, UseInt64: true}).Froze().Unmarshal([]byte(`{"n":1}`), &numberOut); err != nil {
		t.Fatalf("UseNumber+UseInt64 Unmarshal error = %v", err)
	}
	if got, ok := numberOut["n"].(json.Number); !ok || got.String() != "1" {
		t.Fatalf("UseNumber+UseInt64 n = %v (%T), want json.Number(1)", numberOut["n"], numberOut["n"])
	}
}

func TestJSONV2RootStreams(t *testing.T) {
	var buf bytes.Buffer
	enc := Config{NoEncoderNewline: true}.Froze().NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	enc.SetIndent("PFX", "IX")
	if err := enc.Encode(map[string]string{"x": "<tag>"}); err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if got := buf.String(); strings.HasSuffix(got, "\n") {
		t.Fatalf("Encode added trailing newline: %q", got)
	} else if !strings.Contains(got, `\u003ctag\u003e`) || !strings.Contains(got, "\nPFXIX") {
		t.Fatalf("Encode = %q, want escaped and indented output", got)
	}

	dec := Config{UseInt64: true}.Froze().NewDecoder(strings.NewReader(`{"n":1}`))
	dec.UseNumber()
	var out map[string]interface{}
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if got, ok := out["n"].(json.Number); !ok || got.String() != "1" {
		t.Fatalf("decoded n = %v (%T), want json.Number(1)", out["n"], out["n"])
	}
	if dec.Buffered() == nil {
		t.Fatal("Buffered returned nil")
	}
	if dec.More() {
		t.Fatal("More = true after single-value stream, want false")
	}
}
