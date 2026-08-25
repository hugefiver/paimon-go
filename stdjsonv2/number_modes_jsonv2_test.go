//go:build goexperiment.jsonv2

package stdjsonv2

import (
	"encoding/json"
	stdjsontext "encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type numberModesJSONv2Custom struct {
	Calls int
	Value string
}

func (c *numberModesJSONv2Custom) UnmarshalJSON(data []byte) error {
	c.Calls++
	var value struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	c.Value = value.Value
	return nil
}

func assertNumberModeValue(t *testing.T, got any, want string, useInt64 bool) {
	t.Helper()
	if useInt64 {
		wantInt64, err := strconv.ParseInt(want, 10, 64)
		if err != nil {
			t.Fatalf("invalid integer expectation %q: %v", want, err)
		}
		if number, ok := got.(int64); !ok || number != wantInt64 {
			t.Fatalf("number = %#v (%T), want int64(%s)", got, got, want)
		}
		return
	}
	if number, ok := got.(json.Number); !ok || number.String() != want {
		t.Fatalf("number = %#v (%T), want json.Number(%s)", got, got, want)
	}
}

func TestJSONv2NumberModesDecodeNestedInterfaces(t *testing.T) {
	for _, tt := range []struct {
		name     string
		cfg      Config
		useInt64 bool
	}{
		{name: "UseNumber", cfg: Config{UseNumber: true}},
		{name: "UseInt64", cfg: Config{UseInt64: true}, useInt64: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out map[string]any
			if err := tt.cfg.Froze().Unmarshal([]byte(`{"object":{"n":1},"slice":[2]}`), &out); err != nil {
				t.Fatalf("Unmarshal error = %v", err)
			}
			object, ok := out["object"].(map[string]any)
			if !ok {
				t.Fatalf("object = %#v (%T), want map[string]any", out["object"], out["object"])
			}
			assertNumberModeValue(t, object["n"], "1", tt.useInt64)
			slice, ok := out["slice"].([]any)
			if !ok || len(slice) != 1 {
				t.Fatalf("slice = %#v (%T), want one-element []any", out["slice"], out["slice"])
			}
			assertNumberModeValue(t, slice[0], "2", tt.useInt64)
		})
	}
}

func TestJSONv2NumberModesNilAnyObjectRecursion(t *testing.T) {
	for _, tt := range []struct {
		name     string
		cfg      Config
		useInt64 bool
	}{
		{name: "UseNumber", cfg: Config{UseNumber: true}},
		{name: "UseInt64", cfg: Config{UseInt64: true}, useInt64: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := tt.cfg.Froze()

			var nested any
			if err := api.Unmarshal([]byte(`{"object":{"n":1},"slice":[2]}`), &nested); err != nil {
				t.Fatalf("nested object Unmarshal error = %v", err)
			}
			root, ok := nested.(map[string]any)
			if !ok {
				t.Fatalf("nested object = %#v (%T), want map[string]any", nested, nested)
			}
			object, ok := root["object"].(map[string]any)
			if !ok {
				t.Fatalf("object = %#v (%T), want map[string]any", root["object"], root["object"])
			}
			assertNumberModeValue(t, object["n"], "1", tt.useInt64)
			slice, ok := root["slice"].([]any)
			if !ok || len(slice) != 1 {
				t.Fatalf("slice = %#v (%T), want one-element []any", root["slice"], root["slice"])
			}
			assertNumberModeValue(t, slice[0], "2", tt.useInt64)

			var duplicated any
			if err := api.Unmarshal([]byte(`{"value":{"old":1},"value":{"new":2}}`), &duplicated); err != nil {
				t.Fatalf("duplicate object Unmarshal error = %v", err)
			}
			root, ok = duplicated.(map[string]any)
			if !ok {
				t.Fatalf("duplicate object = %#v (%T), want map[string]any", duplicated, duplicated)
			}
			merged, ok := root["value"].(map[string]any)
			if !ok {
				t.Fatalf("value = %#v (%T), want map[string]any", root["value"], root["value"])
			}
			assertNumberModeValue(t, merged["old"], "1", tt.useInt64)
			assertNumberModeValue(t, merged["new"], "2", tt.useInt64)
		})
	}
}

func TestJSONv2NumberModesPreserveJSONv2Options(t *testing.T) {
	type target struct {
		Known any `json:"known"`
	}
	for _, tt := range []struct {
		name string
		cfg  Config
	}{
		{name: "UseNumber", cfg: Config{UseNumber: true, CaseSensitive: true, DisallowUnknownFields: true}},
		{name: "UseInt64", cfg: Config{UseInt64: true, CaseSensitive: true, DisallowUnknownFields: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out target
			if err := tt.cfg.Froze().Unmarshal([]byte(`{"KNOWN":1}`), &out); err == nil {
				t.Fatal("Unmarshal accepted a mismatched-case unknown field")
			}
		})
	}
}

func TestJSONv2NumberModesCallCustomUnmarshalJSONOnce(t *testing.T) {
	for _, tt := range []struct {
		name string
		cfg  Config
	}{
		{name: "UseNumber", cfg: Config{UseNumber: true}},
		{name: "UseInt64", cfg: Config{UseInt64: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out struct {
				Value  any                     `json:"value"`
				Custom numberModesJSONv2Custom `json:"custom"`
			}
			if err := tt.cfg.Froze().Unmarshal([]byte(`{"value":1,"custom":{"value":"ok"}}`), &out); err != nil {
				t.Fatalf("Unmarshal error = %v", err)
			}
			if out.Custom.Calls != 1 || out.Custom.Value != "ok" {
				t.Fatalf("custom = %#v, want one call with value ok", out.Custom)
			}
		})
	}
}

func TestJSONv2NumberModesDuplicateValuesFollowJSONv2Merge(t *testing.T) {
	for _, tt := range []struct {
		name     string
		cfg      Config
		useInt64 bool
	}{
		{name: "UseNumber", cfg: Config{UseNumber: true}},
		{name: "UseInt64", cfg: Config{UseInt64: true}, useInt64: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var scalar any
			api := tt.cfg.Froze()
			if err := api.Unmarshal([]byte(`1`), &scalar); err != nil {
				t.Fatalf("first scalar Unmarshal error = %v", err)
			}
			if err := api.Unmarshal([]byte(`2`), &scalar); err != nil {
				t.Fatalf("second scalar Unmarshal error = %v", err)
			}
			assertNumberModeValue(t, scalar, "2", tt.useInt64)

			var object map[string]any
			if err := api.Unmarshal([]byte(`{"value":{"old":1},"value":{"new":2}}`), &object); err != nil {
				t.Fatalf("duplicate object Unmarshal error = %v", err)
			}
			merged, ok := object["value"].(map[string]any)
			if !ok {
				t.Fatalf("value = %#v (%T), want map[string]any", object["value"], object["value"])
			}
			assertNumberModeValue(t, merged["old"], "1", tt.useInt64)
			assertNumberModeValue(t, merged["new"], "2", tt.useInt64)
		})
	}
}

func TestJSONv2NumberModesReplaceInvalidUTF8(t *testing.T) {
	for _, cfg := range []Config{{UseNumber: true}, {UseInt64: true}} {
		var out map[string]any
		if err := cfg.Froze().Unmarshal([]byte("{\"text\":\"\xff\"}"), &out); err != nil {
			t.Fatalf("Unmarshal invalid UTF-8 error = %v", err)
		}
		if got, ok := out["text"].(string); !ok || got != "�" {
			t.Fatalf("text = %#v (%T), want replacement-rune string", out["text"], out["text"])
		}
	}
}

func TestJSONv2NumberModesPreservePrepopulatedConcreteInterfaces(t *testing.T) {
	for _, tt := range []struct {
		name     string
		cfg      Config
		useInt64 bool
	}{
		{name: "UseNumber", cfg: Config{UseNumber: true}},
		{name: "UseInt64", cfg: Config{UseInt64: true}, useInt64: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := tt.cfg.Froze()

			var mapOut any = map[string]any{"old": "keep"}
			if err := api.Unmarshal([]byte(`{"new":1}`), &mapOut); err != nil {
				t.Fatalf("prepopulated map Unmarshal error = %v", err)
			}
			mapped, ok := mapOut.(map[string]any)
			if !ok || mapped["old"] != "keep" {
				t.Fatalf("map = %#v (%T), want preserved old member", mapOut, mapOut)
			}
			assertNumberModeValue(t, mapped["new"], "1", tt.useInt64)

			var sliceOut any = []any{"old"}
			if err := api.Unmarshal([]byte(`[1]`), &sliceOut); err != nil {
				t.Fatalf("prepopulated slice Unmarshal error = %v", err)
			}
			sliced, ok := sliceOut.([]any)
			if !ok || len(sliced) != 1 {
				t.Fatalf("slice = %#v (%T), want reset one-element []any", sliceOut, sliceOut)
			}
			assertNumberModeValue(t, sliced[0], "1", tt.useInt64)

			var scalarOut any = 1
			if err := api.Unmarshal([]byte(`2`), &scalarOut); err != nil {
				t.Fatalf("prepopulated scalar Unmarshal error = %v", err)
			}
			if got, ok := scalarOut.(int); !ok || got != 2 {
				t.Fatalf("scalar = %#v (%T), want int(2)", scalarOut, scalarOut)
			}

			custom := &numberModesJSONv2Custom{}
			var customOut any = custom
			if err := api.Unmarshal([]byte(`{"value":"ok"}`), &customOut); err != nil {
				t.Fatalf("prepopulated custom pointer Unmarshal error = %v", err)
			}
			if customOut != custom || custom.Calls != 1 || custom.Value != "ok" {
				t.Fatalf("custom = %#v (%T), want original pointer called once", customOut, customOut)
			}
		})
	}
}

func TestJSONv2NumberModesPreservePrepopulatedJSONNumberAcrossKinds(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		target func() any
	}{
		{
			name:   "object",
			input:  `{"new":1}`,
			target: func() any { return json.Number("before") },
		},
		{
			name:   "string",
			input:  `"after"`,
			target: func() any { return json.Number("before") },
		},
		{
			name:  "duplicate number then object",
			input: `{"value":1,"value":{"new":2}}`,
			target: func() any {
				return map[string]any{"value": json.Number("before")}
			},
		},
		{
			name:  "duplicate number then string",
			input: `{"value":1,"value":"after"}`,
			target: func() any {
				return map[string]any{"value": json.Number("before")}
			},
		},
	}

	for _, mode := range []struct {
		name string
		cfg  Config
	}{
		{name: "UseNumber", cfg: Config{UseNumber: true}},
		{name: "UseInt64", cfg: Config{UseInt64: true}},
	} {
		for _, tt := range tests {
			t.Run(mode.name+"/"+tt.name, func(t *testing.T) {
				want := tt.target()
				wantErr := jsonv2.Unmarshal([]byte(tt.input), &want,
					jsonv2.DefaultOptionsV2(),
					stdjsontext.AllowDuplicateNames(true),
					stdjsontext.AllowInvalidUTF8(true),
				)

				got := tt.target()
				gotErr := mode.cfg.Froze().Unmarshal([]byte(tt.input), &got)
				if (gotErr == nil) != (wantErr == nil) || reflect.TypeOf(gotErr) != reflect.TypeOf(wantErr) {
					t.Fatalf("Unmarshal error = %T %v; direct JSON-v2 = %T %v", gotErr, gotErr, wantErr, wantErr)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("Unmarshal output = %#v (%T); direct JSON-v2 = %#v (%T)", got, got, want, want)
				}
			})
		}
	}
}

func TestJSONv2NumberModesStreamUseNumberSuccessiveValues(t *testing.T) {
	dec := Config{UseNumber: true}.Froze().NewDecoder(strings.NewReader(`1 2`))
	for _, want := range []string{"1", "2"} {
		var out any
		if err := dec.Decode(&out); err != nil {
			t.Fatalf("Decode(%s) error = %v", want, err)
		}
		assertNumberModeValue(t, out, want, false)
	}
}

func TestJSONv2NumberModesUseNumberTakesPrecedence(t *testing.T) {
	api := Config{UseNumber: true, UseInt64: true}.Froze()
	var out map[string]any
	if err := api.Unmarshal([]byte(`{"n":1,"slice":[2]}`), &out); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	assertNumberModeValue(t, out["n"], "1", false)
	slice, ok := out["slice"].([]any)
	if !ok || len(slice) != 1 {
		t.Fatalf("slice = %#v (%T), want one-element []any", out["slice"], out["slice"])
	}
	assertNumberModeValue(t, slice[0], "2", false)
}

func TestJSONv2NumberModesConcurrent(t *testing.T) {
	for _, cfg := range []Config{{UseNumber: true}, {UseInt64: true}} {
		api := cfg.Froze()
		const goroutines = 32
		const iterations = 32
		errs := make(chan error, goroutines)
		var workers sync.WaitGroup
		workers.Add(goroutines)
		for range goroutines {
			go func() {
				defer workers.Done()
				for range iterations {
					var out map[string]any
					if err := api.Unmarshal([]byte(`{"n":1,"slice":[2]}`), &out); err != nil {
						errs <- err
						return
					}
				}
			}()
		}
		workers.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
	}
}
