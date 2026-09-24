//go:build goexperiment.jsonv2

package stdjsonv2

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
)

type sonicPointerMarshaler int

func (*sonicPointerMarshaler) MarshalJSON() ([]byte, error) { return []byte(`"pointer"`), nil }

type sonicMapKey int

func (sonicMapKey) MarshalJSON() ([]byte, error) { return []byte(`"json-key"`), nil }
func (sonicMapKey) MarshalText() ([]byte, error) { return []byte("text-key"), nil }

// The expected values were checked against Sonic v1.15.2's native backend.
func TestJSONv2SonicOrdinaryMarshalSemantics(t *testing.T) {
	p := sonicPointerMarshaler(5)
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"omitempty", struct {
			B bool `json:"b,omitempty"`
			I int  `json:"i,omitempty"`
		}{}, `{}`},
		{"byte array", [3]byte{1, 2, 3}, `[1,2,3]`},
		{"value pointer method", p, `5`},
		{"pointer method", &p, `"pointer"`},
		{"map value pointer method", map[string]sonicPointerMarshaler{"x": p}, `{"x":5}`},
		{"map key methods", map[sonicMapKey]int{1: 2}, `{"text-key":2}`},
		{"raw duplicate names", json.RawMessage(`{"x":1,"x":2}`), `{"x":1,"x":2}`},
		{"legacy string tags", struct {
			B bool   `json:"b,string"`
			S string `json:"s,string"`
		}{true, "x"}, `{"b":"true","s":"\"x\""}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, api := range []API{ConfigDefault, ConfigStd, ConfigFastest} {
				got, err := api.Marshal(tt.value)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != tt.want {
					t.Fatalf("Marshal=%s, want %s", got, tt.want)
				}
				var buf bytes.Buffer
				if err := api.NewEncoder(&buf).Encode(tt.value); err != nil {
					t.Fatal(err)
				}
				if got := strings.TrimSuffix(buf.String(), "\n"); got != tt.want {
					t.Fatalf("Encode=%s, want %s", got, tt.want)
				}
			}
		})
	}
}

func TestJSONv2SonicNullAndArraySemantics(t *testing.T) {
	for _, api := range []API{ConfigDefault, Config{UseNumber: true}.Froze(), Config{UseInt64: true}.Froze()} {
		dst := struct {
			N int
			B bool
			S string
			A [3]int
		}{7, true, "keep", [3]int{1, 2, 3}}
		if err := api.Unmarshal([]byte(`{"N":null,"B":null,"S":null,"A":null}`), &dst); err != nil {
			t.Fatal(err)
		}
		want := struct {
			N int
			B bool
			S string
			A [3]int
		}{7, true, "keep", [3]int{1, 2, 3}}
		if dst != want {
			t.Fatalf("null changed scalar/array: %+v", dst)
		}
		if err := api.Unmarshal([]byte(`[9]`), &dst.A); err != nil {
			t.Fatal(err)
		}
		if dst.A != [3]int{9, 0, 0} {
			t.Fatalf("short array=%v", dst.A)
		}
		if err := api.Unmarshal([]byte(`[1,2,3,4]`), &dst.A); err != nil {
			t.Fatal(err)
		}
		if dst.A != [3]int{1, 2, 3} {
			t.Fatalf("long array=%v", dst.A)
		}
	}
}

func TestJSONv2SonicCaseMatchingPreservesDelimiters(t *testing.T) {
	for _, cfg := range []Config{{}, {UseNumber: true}, {UseInt64: true}} {
		var dst struct {
			UserID any `json:"user_id"`
		}
		api := cfg.Froze()
		if err := api.Unmarshal([]byte(`{"userid":1,"user-id":2}`), &dst); err != nil {
			t.Fatal(err)
		}
		if dst.UserID != nil {
			t.Fatalf("matched key without exact delimiters: %#v", dst.UserID)
		}
		if err := api.Unmarshal([]byte(`{"USER_ID":3}`), &dst); err != nil {
			t.Fatal(err)
		}
		if dst.UserID == nil {
			t.Fatal("did not match folded exact-delimiter key")
		}
	}
}

func TestJSONv2SonicUseInt64Numbers(t *testing.T) {
	api := Config{UseInt64: true}.Froze()
	for _, tt := range []struct {
		src  string
		want any
	}{
		{`0`, int64(0)}, {`-0`, int64(0)}, {`9223372036854775807`, int64(math.MaxInt64)},
		{`-9223372036854775808`, int64(math.MinInt64)}, {`1.5`, float64(1.5)},
		{`1e3`, float64(1000)}, {`9223372036854775808`, float64(9223372036854775808)},
	} {
		t.Run(tt.src, func(t *testing.T) {
			var dst any
			if err := api.Unmarshal([]byte(tt.src), &dst); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(dst, tt.want) {
				t.Fatalf("got %#v (%T), want %#v (%T)", dst, dst, tt.want, tt.want)
			}
			if err := api.NewDecoder(strings.NewReader(tt.src)).Decode(&dst); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(dst, tt.want) {
				t.Fatalf("stream got %#v (%T), want %#v (%T)", dst, dst, tt.want, tt.want)
			}
		})
	}
	var dst any
	if err := api.Unmarshal([]byte(`1e400`), &dst); err == nil {
		t.Fatal("accepted overflowing float")
	}
	if err := api.NewDecoder(strings.NewReader(`1e400`)).Decode(&dst); err == nil {
		t.Fatal("stream accepted overflowing float")
	}
	if err := (Config{UseNumber: true}).Froze().Unmarshal([]byte(`1e400`), &dst); err != nil {
		t.Fatal(err)
	}
	if dst != json.Number("1e400") {
		t.Fatalf("UseNumber=%#v", dst)
	}
}

type sonicUntouchedCustom struct {
	Number any
	Calls  int
}

func (s *sonicUntouchedCustom) UnmarshalJSON([]byte) error {
	s.Calls++
	s.Number = json.Number("123")
	return nil
}
func TestJSONv2UseInt64OnlyTouchesDecodedNumbers(t *testing.T) {
	cycle := map[string]any{}
	cycle["self"] = cycle
	dst := struct {
		Untouched any
		Ignored   any `json:"-"`
		Custom    sonicUntouchedCustom
		N         any
	}{
		Untouched: cycle, Ignored: json.Number("44"),
	}
	if err := (Config{UseInt64: true}).Froze().Unmarshal([]byte(`{"N":1,"Custom":0,"Ignored":2}`), &dst); err != nil {
		t.Fatal(err)
	}
	if dst.N != int64(1) || dst.Ignored != json.Number("44") || dst.Custom.Number != json.Number("123") || dst.Custom.Calls != 1 {
		t.Fatalf("unexpected destination: N=%#v Ignored=%#v Custom=%+v", dst.N, dst.Ignored, dst.Custom)
	}
	if reflect.ValueOf(dst.Untouched).Pointer() != reflect.ValueOf(cycle).Pointer() {
		t.Fatal("replaced untouched map")
	}
}

func TestJSONv2NumberModeConcretePointers(t *testing.T) {
	for _, cfg := range []Config{{UseNumber: true}, {UseInt64: true}} {
		n := 7
		var dst any = &n
		if err := cfg.Froze().Unmarshal([]byte(`2`), &dst); err != nil {
			t.Fatal(err)
		}
		if dst != &n || n != 2 {
			t.Fatalf("concrete pointer changed: %#v", dst)
		}
		custom := &sonicUntouchedCustom{}
		dst = custom
		if err := cfg.Froze().Unmarshal([]byte(`2`), &dst); err != nil {
			t.Fatal(err)
		}
		if dst != custom || custom.Calls != 1 || custom.Number != json.Number("123") {
			t.Fatalf("custom pointer changed: %#v", dst)
		}
	}
}

func TestJSONv2StreamingNumberOwnershipAndModeChanges(t *testing.T) {
	dec := Config{UseInt64: true}.Froze().NewDecoder(strings.NewReader(`1 123456789012345678901234567890 3`))
	var a, b, c any
	if err := dec.Decode(&a); err != nil {
		t.Fatal(err)
	}
	dec.UseNumber()
	if err := dec.Decode(&b); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&c); err != nil {
		t.Fatal(err)
	}
	if a != int64(1) || b != json.Number("123456789012345678901234567890") || c != json.Number("3") {
		t.Fatalf("stream values=%#v %#v %#v", a, b, c)
	}
	if err := dec.Decode(&c); err != io.EOF {
		t.Fatalf("EOF error=%v", err)
	}
}

func TestJSONv2StreamEncoderRecoversWithoutPartialWrites(t *testing.T) {
	var buf bytes.Buffer
	enc := ConfigDefault.NewEncoder(&buf)
	if err := enc.Encode(struct {
		Good int
		Bad  chan int
	}{Good: 1, Bad: make(chan int)}); err == nil {
		t.Fatal("accepted channel")
	}
	if buf.Len() != 0 {
		t.Fatalf("published partial output %q", buf.String())
	}
	if err := enc.Encode(map[string]int{"ok": 2}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != `{"ok":2}`+"\n" {
		t.Fatalf("output after error=%q", buf.String())
	}
}

func TestJSONv2StreamEncoderOptionsAndNewline(t *testing.T) {
	var plain, escaped bytes.Buffer
	api := Config{NoEncoderNewline: true}.Froze()
	a, b := api.NewEncoder(&plain), api.NewEncoder(&escaped)
	b.SetEscapeHTML(true)
	for range 2 {
		if err := a.Encode("<"); err != nil {
			t.Fatal(err)
		}
		if err := b.Encode("<"); err != nil {
			t.Fatal(err)
		}
	}
	if plain.String() != `"<""<"` || escaped.String() != `"\u003c""\u003c"` {
		t.Fatalf("plain=%q escaped=%q", plain.String(), escaped.String())
	}
	a.SetIndent("P", " ")
	plain.Reset()
	if err := a.Encode(map[string]int{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if plain.String() != "{\nP \"n\": 1\nP}" {
		t.Fatalf("indent=%q", plain.String())
	}
}

func TestJSONv2SelfReferentialInterfacePointer(t *testing.T) {
	for _, tt := range []struct {
		name   string
		cfg    Config
		number any
	}{
		{"default", Config{}, float64(1)},
		{"number", Config{UseNumber: true}, json.Number("1")},
		{"int64", Config{UseInt64: true}, int64(1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := tt.cfg.Froze()
			for _, src := range []string{`1`, `{"n":1}`} {
				var dst any
				dst = &dst
				if err := api.Unmarshal([]byte(src), &dst); err != nil {
					t.Fatal(err)
				}
				want := tt.number
				if src[0] == '{' {
					want = map[string]any{"n": tt.number}
				}
				if !reflect.DeepEqual(dst, want) {
					t.Fatalf("self pointer result=%#v, want %#v", dst, want)
				}
			}
			var dst struct{ Value any }
			dst.Value = &dst.Value
			if err := api.Unmarshal([]byte(`{"Value":1}`), &dst); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(dst.Value, tt.number) {
				t.Fatalf("field self pointer=%#v", dst.Value)
			}
			var stream any
			stream = &stream
			if err := api.NewDecoder(strings.NewReader(`1`)).Decode(&stream); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(stream, tt.number) {
				t.Fatalf("stream self pointer=%#v", stream)
			}
		})
	}
}

type sonicFailingUnmarshaler struct{ Calls int }

var errSonicCustom = errors.New("custom decode failed")

func (v *sonicFailingUnmarshaler) UnmarshalJSON([]byte) error { v.Calls++; return errSonicCustom }

func TestJSONv2CustomErrorsStopDecoding(t *testing.T) {
	for _, cfg := range []Config{{}, {UseNumber: true}, {UseInt64: true}} {
		var dst struct {
			Bad   sonicFailingUnmarshaler
			After any
		}
		err := cfg.Froze().Unmarshal([]byte(`{"Bad":1,"After":2}`), &dst)
		if !errors.Is(err, errSonicCustom) {
			t.Fatalf("error=%v", err)
		}
		if dst.Bad.Calls != 1 || dst.After != nil {
			t.Fatalf("decoded after custom failure: %+v", dst)
		}
		var unknown struct{ After any }
		cfg.DisallowUnknownFields = true
		if err := cfg.Froze().Unmarshal([]byte(`{"missing":1,"After":2}`), &unknown); err == nil {
			t.Fatal("accepted unknown field")
		}
		if unknown.After != nil {
			t.Fatalf("decoded after unknown field: %+v", unknown)
		}
	}
}

func TestJSONv2StreamDecodeErrorsAreSticky(t *testing.T) {
	for _, cfg := range []Config{{}, {UseNumber: true}, {UseInt64: true}} {
		var dst struct {
			Bad   sonicFailingUnmarshaler
			After any
		}
		dec := cfg.Froze().NewDecoder(strings.NewReader(`{"Bad":1,"After":2} 7`))
		first := dec.Decode(&dst)
		if !errors.Is(first, errSonicCustom) {
			t.Fatalf("error=%v", first)
		}
		var next any
		if err := dec.Decode(&next); err != first {
			t.Fatalf("second error=%v, first=%v", err, first)
		}
		if dst.Bad.Calls != 1 || dst.After != nil || next != nil {
			t.Fatalf("decoded after error: %+v; next=%#v", dst, next)
		}
		cfg.DisallowUnknownFields = true
		dec = cfg.Froze().NewDecoder(strings.NewReader(`{"missing":1,"After":2} 7`))
		first = dec.Decode(&dst)
		if first == nil {
			t.Fatal("accepted unknown field")
		}
		if err := dec.Decode(&next); err != first {
			t.Fatalf("second unknown error=%v, first=%v", err, first)
		}
	}
}

func TestJSONv2MultiInterfacePointerCycleReturnsError(t *testing.T) {
	for _, cfg := range []Config{{}, {UseNumber: true}, {UseInt64: true}} {
		var a, b, c any
		a = &b
		b = &c
		c = &a
		err := cfg.Froze().Unmarshal([]byte(`1`), &a)
		if !errors.Is(err, errCyclicInterfacePointer) {
			t.Fatalf("cycle error=%v", err)
		}
	}
}
