package jsonconv

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestNumberKindsMatchNativeSonic(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{`0`, int64(0)}, {`-0`, int64(0)}, {`9223372036854775807`, int64(math.MaxInt64)},
		{`-9223372036854775808`, int64(math.MinInt64)}, {`1.5`, float64(1.5)}, {`1e3`, float64(1000)},
		{`9223372036854775808`, float64(9223372036854775808.0)},
	}
	for _, tt := range cases {
		t.Run(tt.src, func(t *testing.T) {
			var v any
			if err := UnmarshalInt64([]byte(tt.src), &v, false); err != nil || !reflect.DeepEqual(v, tt.want) {
				t.Fatalf("decode=%v (%T), %v; want %v (%T)", v, v, err, tt.want, tt.want)
			}
		})
	}
	var v any
	if err := UnmarshalInt64([]byte(`1e400`), &v, false); err == nil {
		t.Fatal("floating-point overflow accepted")
	}
}

type customNumber struct {
	calls int
	Value any
}

func (v *customNumber) UnmarshalJSON([]byte) error {
	v.calls++
	v.Value = json.Number("91")
	return nil
}

type textKey string

var keyCalls int

func (k *textKey) UnmarshalText(s []byte) error {
	keyCalls++
	*k = textKey(strings.ToUpper(string(s)))
	return nil
}

func TestOnlyInputValuesAreDecoded(t *testing.T) {
	type state struct {
		Value   any
		Keep    any `json:"-"`
		Omitted any
		Self    *state `json:"-"`
		Custom  customNumber
		Map     map[textKey]any
	}
	s := state{Keep: json.Number("9"), Omitted: json.Number("8"), Map: map[textKey]any{"OLD": json.Number("7")}}
	s.Self = &s
	keyCalls = 0
	if err := UnmarshalInt64([]byte(`{"Value":[1,1.5],"Custom":{},"Map":{"new":2}}`), &s, false); err != nil {
		t.Fatal(err)
	}
	if s.Keep != json.Number("9") || s.Omitted != json.Number("8") || s.Self != &s {
		t.Fatalf("untouched fields changed: %#v", s)
	}
	if s.Custom.calls != 1 || s.Custom.Value != json.Number("91") {
		t.Fatalf("custom decoder result changed: %#v", s.Custom)
	}
	if keyCalls != 1 || s.Map["NEW"] != int64(2) || s.Map["OLD"] != json.Number("7") {
		t.Fatalf("custom keys or omitted map members changed: calls=%d map=%#v", keyCalls, s.Map)
	}
	if !reflect.DeepEqual(s.Value, []any{int64(1), float64(1.5)}) {
		t.Fatalf("Value=%#v", s.Value)
	}
}

func TestPrepopulatedInterfacesAndRawMessages(t *testing.T) {
	c := &customNumber{}
	var v any = c
	if err := UnmarshalInt64([]byte(`123`), &v, false); err != nil || v != c || c.calls != 1 || c.Value != json.Number("91") {
		t.Fatalf("custom pointer: %#v %v", v, err)
	}
	n := json.Number("0")
	v = &n
	if err := UnmarshalInt64([]byte(`1.5`), &v, false); err != nil || v != &n || n != "1.5" {
		t.Fatalf("number pointer: %#v %v", v, err)
	}
	v = json.Number("0")
	if err := UnmarshalInt64([]byte(`2`), &v, false); err != nil || v != int64(2) {
		t.Fatalf("replace scalar: %#v %v", v, err)
	}
	raw := json.RawMessage(nil)
	v = &raw
	if err := UnmarshalInt64([]byte(`{ "n": 1 }`), &v, false); err != nil || string(raw) != `{ "n": 1 }` {
		t.Fatalf("raw=%s %v", raw, err)
	}
}

type Embedded struct{ Value any }
type Left struct{ Clash any }
type Right struct{ Clash any }

func TestStructFieldSelectionAndDuplicateMerges(t *testing.T) {
	type target struct {
		*Embedded
		Left
		Right
		K       any
		Tagged  any `json:"other"`
		Quoted  int `json:",string"`
		Ignored any `json:"-"`
	}
	var v target
	data := []byte(`{"Value":1,"Value":2,"Clash":3,"K":4,"other":5,"Quoted":"6","Ignored":7}`)
	if err := UnmarshalInt64(data, &v, false); err != nil {
		t.Fatal(err)
	}
	if v.Embedded == nil || v.Value != int64(2) || v.Left.Clash != nil || v.Right.Clash != nil || v.K != int64(4) || v.Tagged != int64(5) || v.Quoted != 6 || v.Ignored != nil {
		t.Fatalf("field selection=%#v", v)
	}
	if err := UnmarshalInt64([]byte(`{"missing":1,"Value":9}`), &v, true); err == nil || v.Value != int64(2) {
		t.Fatalf("unknown field stops=%#v %v", v, err)
	}
	for _, src := range []string{`{"Quoted":""}`, `{"Quoted":"oops"}`, `{"Quoted":1}`} {
		if err := UnmarshalInt64([]byte(src), &v, false); err == nil {
			t.Fatalf("accepted %s", src)
		}
	}
	type part struct {
		A any
		B any
	}
	var merged struct{ P part }
	if err := UnmarshalInt64([]byte(`{"P":{"A":1},"P":{"B":2}}`), &merged, false); err != nil || merged.P.A != int64(1) || merged.P.B != int64(2) {
		t.Fatalf("duplicate struct=%#v %v", merged, err)
	}
}

func TestArrayAndSliceReuse(t *testing.T) {
	a := [3]any{json.Number("9"), json.Number("8"), json.Number("7")}
	if err := UnmarshalInt64([]byte(`[1]`), &a, false); err != nil || a != [3]any{int64(1), nil, nil} {
		t.Fatalf("array=%#v %v", a, err)
	}
	type item struct {
		Keep any
		New  any
	}
	s := []item{{Keep: json.Number("9")}}
	if err := UnmarshalInt64([]byte(`[{"New":1}]`), &s, false); err != nil || s[0].Keep != json.Number("9") || s[0].New != int64(1) {
		t.Fatalf("slice reuse=%#v %v", s, err)
	}
	if err := UnmarshalInt64([]byte(`[]`), &s, false); err != nil || s == nil || len(s) != 0 {
		t.Fatalf("empty slice=%#v %v", s, err)
	}
}

func FuzzTypedContainersMatchStandardJSON(f *testing.F) {
	for _, s := range []string{`{}`, `{"N":1,"List":[1,2],"Map":{"1":"a"}}`, `{"Q":"2"}`, `{"List":null}`, `{"N":"bad"}`, `{"Q":""}`} {
		f.Add(s)
	}
	type target struct {
		N    int
		List []int
		Map  map[int]string
		Q    int `json:",string"`
	}
	f.Fuzz(func(t *testing.T, s string) {
		var got, want target
		e1 := UnmarshalInt64([]byte(s), &got, false)
		e2 := json.Unmarshal([]byte(s), &want)
		if (e1 == nil) != (e2 == nil) {
			t.Fatalf("error parity %q: ours=%v std=%v", s, e1, e2)
		}
		if e1 == nil && !reflect.DeepEqual(got, want) {
			t.Fatalf("value parity %q: ours=%#v std=%#v", s, got, want)
		}
	})
}

func TestSelfReferentialInterface(t *testing.T) {
	var v any
	v = &v
	if err := UnmarshalInt64([]byte(`1`), &v, false); err != nil || v != int64(1) {
		t.Fatalf("self reference = %#v, %v", v, err)
	}
}

var stopError = fmt.Errorf("stop custom decoding")
var stopCalls int

type stopValue int

func (*stopValue) UnmarshalJSON([]byte) error { stopCalls++; return stopError }

type stopKey string

func (k *stopKey) UnmarshalText(s []byte) error {
	stopCalls++
	if string(s) == "bad" {
		return stopError
	}
	*k = stopKey(s)
	return nil
}

type quotedRaw int

var quotedInput string

func (v *quotedRaw) UnmarshalJSON(raw []byte) error { quotedInput = string(raw); *v = 9; return nil }

func TestNativeFatalErrorsStopAtCurrentToken(t *testing.T) {
	stopCalls = 0
	var v struct {
		Bad   stopValue
		After any
	}
	if err := UnmarshalInt64([]byte(`{"Bad":1,"After":2}`), &v, false); err != stopError || v.After != nil || stopCalls != 1 {
		t.Fatalf("struct=%#v err=%v calls=%d", v, err, stopCalls)
	}
	stopCalls = 0
	a := []stopValue{7, 8, 9}
	if err := UnmarshalInt64([]byte(`[1,2,3]`), &a, false); err != stopError || len(a) != 1 || a[0] != 7 || stopCalls != 1 {
		t.Fatalf("slice=%#v err=%v calls=%d", a, err, stopCalls)
	}
	stopCalls = 0
	var m map[stopKey]any
	if err := UnmarshalInt64([]byte(`{"bad":1,"good":2}`), &m, false); err != stopError || len(m) != 0 || stopCalls != 1 {
		t.Fatalf("map=%#v err=%v calls=%d", m, err, stopCalls)
	}
	var numbers map[string]any
	if err := UnmarshalInt64([]byte(`{"ok":1,"bad":1e400,"after":2}`), &numbers, false); err == nil || numbers["ok"] != int64(1) || len(numbers) != 2 {
		t.Fatalf("overflow=%#v %v", numbers, err)
	}
}

func TestNativeCustomStringTagOwnsUnquotedData(t *testing.T) {
	var v struct {
		Q     quotedRaw `json:",string"`
		After any
	}
	if err := UnmarshalInt64([]byte(`{"Q":"abc","After":2}`), &v, false); err != nil || quotedInput != "abc" || v.Q != 9 || v.After != int64(2) {
		t.Fatalf("quoted=%#v input=%q err=%v", v, quotedInput, err)
	}
	var invalidTag struct {
		N any `json:"bad\\name"`
	}
	if err := UnmarshalInt64([]byte(`{"N":1}`), &invalidTag, false); err != nil || invalidTag.N != int64(1) {
		t.Fatalf("invalid tag=%#v %v", invalidTag, err)
	}
}

func BenchmarkInt64NestedArrays(b *testing.B) {
	for _, depth := range []int{64, 256, 1024} {
		b.Run(fmt.Sprint(depth), func(b *testing.B) {
			raw := []byte(strings.Repeat("[", depth) + "1" + strings.Repeat("]", depth))
			b.ReportAllocs()
			for b.Loop() {
				var v any
				if err := UnmarshalInt64(raw, &v, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
