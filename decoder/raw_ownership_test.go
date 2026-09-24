package decoder_test

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/decoder"
)

type rawEnvelope struct{ Raw sonic.NoCopyRawMessage }
type rawDecoder interface{ Decode(any) error }

func TestNoCopyRawMessageSurvivesDecoderRefills(t *testing.T) {
	shapes := []struct {
		name, first string
		makeTarget  func() (any, func() string)
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
	for _, mode := range []decoder.Options{0, decoder.OptionUseInt64, decoder.OptionUseNumber} {
		constructors := []struct {
			name string
			new  func(string) rawDecoder
		}{
			{"string", func(src string) rawDecoder { d := decoder.NewDecoder(src); d.SetOptions(mode); return d }},
			{"stream", func(src string) rawDecoder {
				d := decoder.NewStreamDecoder(strings.NewReader(src))
				d.SetOptions(mode)
				return d
			}},
			{"root", func(src string) rawDecoder {
				return (sonic.Config{UseInt64: mode == decoder.OptionUseInt64, UseNumber: mode == decoder.OptionUseNumber}).Froze().NewDecoder(strings.NewReader(src))
			}},
		}
		for _, ctor := range constructors {
			for _, shape := range shapes {
				t.Run(ctor.name+"/"+shape.name+"/"+string(rune('0'+mode)), func(t *testing.T) {
					middle := strings.Repeat("x", 2048)
					d := ctor.new(shape.first + ` "` + middle + `" 9`)
					dst, get := shape.makeTarget()
					if err := d.Decode(dst); err != nil {
						t.Fatal(err)
					}
					if got := get(); got != `{"first":1}` {
						t.Fatalf("initial raw=%q", got)
					}
					var s string
					if err := d.Decode(&s); err != nil || s != middle {
						t.Fatalf("middle len=%d err=%v", len(s), err)
					}
					var last int
					if err := d.Decode(&last); err != nil || last != 9 {
						t.Fatalf("last=%d err=%v", last, err)
					}
					if got := get(); got != `{"first":1}` {
						t.Fatalf("retained raw changed across refill: %q", got)
					}
				})
			}
		}
	}
}

func TestNoCopyDetachPreservesStreamModeSwitchAndBuffer(t *testing.T) {
	d := decoder.NewStreamDecoder(strings.NewReader(`{"first":1}  2 3`))
	var raw sonic.NoCopyRawMessage
	if err := d.Decode(&raw); err != nil {
		t.Fatal(err)
	}
	before := d.InputOffset()
	buffered, _ := io.ReadAll(d.Buffered())
	if before != 13 || string(buffered) != "2 3" {
		t.Fatalf("before switch offset=%d buffer=%q", before, buffered)
	}
	d.UseNumber()
	var n any
	if err := d.Decode(&n); err != nil || n != json.Number("2") {
		t.Fatalf("second=%#v %v", n, err)
	}
	d.UseInt64()
	if err := d.Decode(&n); err != nil || n != int64(3) {
		t.Fatalf("third=%#v %v", n, err)
	}
	if d.InputOffset() != 16 || string(raw) != `{"first":1}` {
		t.Fatalf("after switch offset=%d raw=%q", d.InputOffset(), raw)
	}
}
