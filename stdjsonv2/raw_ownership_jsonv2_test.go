//go:build goexperiment.jsonv2

package stdjsonv2_test

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/stdjsonv2"
)

type rawEnvelope struct{ Raw sonic.NoCopyRawMessage }

func TestJSONv2NoCopyRawMessageSurvivesRefills(t *testing.T) {
	shapes := []struct {
		name, first string
		target      func() (any, func() string)
	}{
		{"direct", `{"first":1}`, func() (any, func() string) {
			var v sonic.NoCopyRawMessage
			return &v, func() string { return string(v) }
		}},
		{"struct", `{"Raw":{"first":1}}`, func() (any, func() string) { var v rawEnvelope; return &v, func() string { return string(v.Raw) } }},
		{"slice", `[{"first":1}]`, func() (any, func() string) {
			var v []sonic.NoCopyRawMessage
			return &v, func() string { return string(v[0]) }
		}},
		{"map", `{"key":{"first":1}}`, func() (any, func() string) {
			var v map[string]sonic.NoCopyRawMessage
			return &v, func() string { return string(v["key"]) }
		}},
		{"interface", `{"Raw":{"first":1}}`, func() (any, func() string) {
			v := new(rawEnvelope)
			var out any = v
			return &out, func() string { return string(v.Raw) }
		}},
	}
	for _, mode := range []struct {
		name string
		cfg  stdjsonv2.Config
	}{
		{"default", stdjsonv2.Config{}}, {"number", stdjsonv2.Config{UseNumber: true}}, {"int64", stdjsonv2.Config{UseInt64: true}},
	} {
		for _, shape := range shapes {
			t.Run(mode.name+"/"+shape.name, func(t *testing.T) {
				middle := strings.Repeat("x", 2048)
				dec := mode.cfg.Froze().NewDecoder(strings.NewReader(shape.first + ` "` + middle + `" 9`))
				dst, raw := shape.target()
				if err := dec.Decode(dst); err != nil {
					t.Fatal(err)
				}
				if raw() != `{"first":1}` {
					t.Fatalf("initial raw=%q", raw())
				}
				// More may refill the reader too; retaining the raw value must not depend
				// on the next operation being Decode rather than More or Buffered.
				if !dec.More() {
					t.Fatal("missing middle value")
				}
				snapshot := dec.Buffered()
				var s string
				if err := dec.Decode(&s); err != nil || s != middle {
					t.Fatalf("middle length=%d error=%v", len(s), err)
				}
				var last int
				if err := dec.Decode(&last); err != nil || last != 9 {
					t.Fatalf("last=%d error=%v", last, err)
				}
				if raw() != `{"first":1}` {
					t.Fatalf("retained raw changed=%q", raw())
				}
				if _, err := io.ReadAll(snapshot); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestJSONv2RawOwnershipPreservesOptionsAndStickyErrors(t *testing.T) {
	dec := stdjsonv2.Config{UseInt64: true}.Froze().NewDecoder(strings.NewReader(`{"first":1} 2 {"missing":3} 4`))
	var raw sonic.NoCopyRawMessage
	if err := dec.Decode(&raw); err != nil {
		t.Fatal(err)
	}
	buffered, err := io.ReadAll(dec.Buffered())
	if err != nil || string(buffered) != `2 {"missing":3} 4` {
		t.Fatalf("buffer=%q error=%v", buffered, err)
	}
	dec.UseNumber()
	var number any
	if err := dec.Decode(&number); err != nil || number != json.Number("2") {
		t.Fatalf("number=%#v error=%v", number, err)
	}
	dec.DisallowUnknownFields()
	var dst struct{ Known any }
	first := dec.Decode(&dst)
	if first == nil {
		t.Fatal("accepted unknown field")
	}
	if err := dec.Decode(&number); err != first {
		t.Fatalf("sticky error=%v first=%v", err, first)
	}
	if dec.More() {
		t.Fatal("More succeeded after terminal error")
	}
	buffered, err = io.ReadAll(dec.Buffered())
	if err != nil || len(buffered) != 0 {
		t.Fatalf("terminal buffer=%q error=%v", buffered, err)
	}
	if string(raw) != `{"first":1}` {
		t.Fatalf("raw changed after mode/error=%q", raw)
	}
}
