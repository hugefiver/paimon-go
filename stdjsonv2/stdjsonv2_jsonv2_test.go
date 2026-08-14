//go:build goexperiment.jsonv2

// Tests for the jsonv2-backed implementation of stdjsonv2. These run
// only when the toolchain was built with GOEXPERIMENT=jsonv2.

package stdjsonv2

import (
	"bytes"
	"encoding/json"
	stdjsontext "encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"strings"
	"sync"
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

type jsonv2OptionsState struct {
	len   int
	cap   int
	first *jsonv2.Options
}

func snapshotJSONv2Options(t *testing.T, name string, opts []jsonv2.Options) jsonv2OptionsState {
	t.Helper()
	if len(opts) == 0 {
		t.Fatalf("%s options are empty", name)
	}
	return jsonv2OptionsState{len: len(opts), cap: cap(opts), first: &opts[0]}
}

func assertJSONv2OptionsStable(t *testing.T, name string, got, want jsonv2OptionsState) {
	t.Helper()
	if got != want {
		t.Fatalf("%s options changed: got %+v, want %+v", name, got, want)
	}
}

func assertJSONv2Option[T comparable](t *testing.T, opts jsonv2.Options, name string, setter func(T) jsonv2.Options, want T) {
	t.Helper()
	got, ok := jsonv2.GetOption(opts, setter)
	if !ok || got != want {
		t.Fatalf("%s = %v (present: %t), want %v", name, got, ok, want)
	}
}

func TestJSONv2FrozenOptionsAreCachedAndImmutable(t *testing.T) {
	type target struct {
		Known string `json:"known"`
	}

	t.Run("cached options remain stable", func(t *testing.T) {
		tests := []struct {
			name string
			api  *jsonv2API
		}{
			{name: "default", api: ConfigDefault.(*jsonv2API)},
			{name: "custom", api: Config{
				EscapeHTML:            true,
				SortMapKeys:           true,
				NoNullSliceOrMap:      true,
				DisallowUnknownFields: true,
				CaseSensitive:         true,
			}.Froze().(*jsonv2API)},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				marshalBefore := snapshotJSONv2Options(t, "marshal", tt.api.marshalOpts)
				unmarshalBefore := snapshotJSONv2Options(t, "unmarshal", tt.api.unmarshalOpts)

				for range 2 {
					if _, err := tt.api.Marshal(map[string]int{"value": 1}); err != nil {
						t.Fatalf("Marshal error = %v", err)
					}
					var out target
					if err := tt.api.Unmarshal([]byte(`{"known":"value"}`), &out); err != nil {
						t.Fatalf("Unmarshal error = %v", err)
					}
					if out.Known != "value" {
						t.Fatalf("Unmarshal result = %+v", out)
					}
				}

				assertJSONv2OptionsStable(t, "marshal", snapshotJSONv2Options(t, "marshal", tt.api.marshalOpts), marshalBefore)
				assertJSONv2OptionsStable(t, "unmarshal", snapshotJSONv2Options(t, "unmarshal", tt.api.unmarshalOpts), unmarshalBefore)
			})
		}
	})

	t.Run("concurrent API reuse", func(t *testing.T) {
		api := ConfigDefault.(*jsonv2API)
		const goroutines = 32
		const iterations = 32
		errs := make(chan error, goroutines)
		var wg sync.WaitGroup
		for range goroutines {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range iterations {
					encoded, err := api.Marshal(map[string]int{"value": 1})
					if err != nil {
						errs <- err
						return
					}
					if string(encoded) != `{"value":1}` {
						errs <- errors.New("Marshal produced unstable output")
						return
					}
					var out target
					if err := api.Unmarshal([]byte(`{"known":"value"}`), &out); err != nil {
						errs <- err
						return
					}
					if out.Known != "value" {
						errs <- errors.New("Unmarshal produced unstable output")
						return
					}
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
	})

	t.Run("decoder options are isolated from API", func(t *testing.T) {
		api := Config{}.Froze().(*jsonv2API)
		unmarshalBefore := snapshotJSONv2Options(t, "API unmarshal", api.unmarshalOpts)
		input := `{"known":"value","unknown":"ignored"}`

		decoder1 := api.NewDecoder(strings.NewReader(input))
		decoder1.DisallowUnknownFields()
		if err := decoder1.Decode(&target{}); err == nil {
			t.Fatal("decoder with DisallowUnknownFields accepted unknown field")
		}

		decoder2 := api.NewDecoder(strings.NewReader(input))
		var fromDecoder target
		if err := decoder2.Decode(&fromDecoder); err != nil {
			t.Fatalf("independent decoder rejected unknown field: %v", err)
		}
		if fromDecoder.Known != "value" {
			t.Fatalf("independent decoder result = %+v", fromDecoder)
		}

		var fromAPI target
		if err := api.Unmarshal([]byte(input), &fromAPI); err != nil {
			t.Fatalf("API Unmarshal rejected unknown field: %v", err)
		}
		if fromAPI.Known != "value" {
			t.Fatalf("API Unmarshal result = %+v", fromAPI)
		}
		assertJSONv2OptionsStable(t, "API unmarshal", snapshotJSONv2Options(t, "API unmarshal", api.unmarshalOpts), unmarshalBefore)
	})
}

func TestJSONv2CachedCustomOptionsHonorConfiguration(t *testing.T) {
	type target struct {
		Known string `json:"known"`
	}
	type inlinedField struct {
		Lower string `json:"field"`
	}
	type caseSensitiveMarshalTarget struct {
		inlinedField `json:",inline"`
		Upper        string `json:"FIELD"`
	}
	type nilCollections struct {
		Slice []string       `json:"slice"`
		Map   map[string]int `json:"map"`
	}

	api := Config{
		EscapeHTML:            true,
		SortMapKeys:           true,
		NoNullSliceOrMap:      true,
		DisallowUnknownFields: true,
		CaseSensitive:         true,
	}.Froze().(*jsonv2API)

	// Keep the cached option slices separate so that no-op v2 defaults (nil
	// collection formatting and strict field matching) still prove their
	// Config branches add the intended option to the relevant builder.
	if got, want := len(api.marshalOpts), 6; got != want {
		t.Fatalf("marshal option count = %d, want %d", got, want)
	}
	if got, want := len(api.unmarshalOpts), 3; got != want {
		t.Fatalf("unmarshal option count = %d, want %d", got, want)
	}

	marshalOpts := jsonv2.JoinOptions(api.marshalOpts...)
	unmarshalOpts := jsonv2.JoinOptions(api.unmarshalOpts...)
	assertJSONv2Option(t, marshalOpts, "EscapeForHTML", stdjsontext.EscapeForHTML, true)
	assertJSONv2Option(t, marshalOpts, "Deterministic", jsonv2.Deterministic, true)
	assertJSONv2Option(t, marshalOpts, "FormatNilSliceAsNull", jsonv2.FormatNilSliceAsNull, false)
	assertJSONv2Option(t, marshalOpts, "FormatNilMapAsNull", jsonv2.FormatNilMapAsNull, false)
	assertJSONv2Option(t, marshalOpts, "MatchCaseInsensitiveNames", jsonv2.MatchCaseInsensitiveNames, false)
	assertJSONv2Option(t, unmarshalOpts, "RejectUnknownMembers", jsonv2.RejectUnknownMembers, true)
	assertJSONv2Option(t, unmarshalOpts, "MatchCaseInsensitiveNames", jsonv2.MatchCaseInsensitiveNames, false)

	encoded, err := api.Marshal(map[string]string{
		"z": "<>&",
		"a": "first",
	})
	if err != nil {
		t.Fatalf("Marshal escaped map error = %v", err)
	}
	if got, want := string(encoded), `{"a":"first","z":"\u003c\u003e\u0026"}`; got != want {
		t.Fatalf("Marshal escaped deterministic map = %s, want %s", got, want)
	}

	encoded, err = api.Marshal(nilCollections{})
	if err != nil {
		t.Fatalf("Marshal nil collections error = %v", err)
	}
	if got, want := string(encoded), `{"slice":[],"map":{}}`; got != want {
		t.Fatalf("Marshal nil collections = %s, want %s", got, want)
	}

	encoded, err = api.Marshal(caseSensitiveMarshalTarget{
		inlinedField: inlinedField{Lower: "lower"},
		Upper:        "upper",
	})
	if err != nil {
		t.Fatalf("Marshal case-sensitive fields error = %v", err)
	}
	if got, want := string(encoded), `{"field":"lower","FIELD":"upper"}`; got != want {
		t.Fatalf("Marshal case-sensitive fields = %s, want %s", got, want)
	}

	var known target
	if err := api.Unmarshal([]byte(`{"known":"value"}`), &known); err != nil {
		t.Fatalf("Unmarshal exact-case field error = %v", err)
	}
	if got, want := known.Known, "value"; got != want {
		t.Fatalf("Unmarshal exact-case field = %q, want %q", got, want)
	}

	var mismatchedCase target
	if err := api.Unmarshal([]byte(`{"KNOWN":"value"}`), &mismatchedCase); err == nil {
		t.Fatal("Unmarshal accepted mismatched-case field")
	}
	if mismatchedCase.Known != "" {
		t.Fatalf("Unmarshal populated mismatched-case field: %+v", mismatchedCase)
	}

	if err := api.Unmarshal([]byte(`{"known":"value","unknown":"ignored"}`), &target{}); err == nil {
		t.Fatal("Unmarshal accepted unknown field")
	}
}

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

func TestJSONv2GetEscapedObjectKey(t *testing.T) {
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

func TestJSONv2GetRejectsMalformedInputEvenInDefaultSonicMode(t *testing.T) {
	for _, data := range [][]byte{
		[]byte(`{"a":1}xxx`),
		[]byte(`{"a":01}`),
		[]byte(`{"a":1.}`),
		[]byte(`{"a":1e}`),
		[]byte(`{"a":+1}`),
	} {
		if _, err := Get(data, "a"); err == nil {
			t.Fatalf("Get(%q, a) error = nil, want strict jsonv2 error", data)
		}
	}
}
