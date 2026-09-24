package jsonconv

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestDynamicFloatNumberBoundaries(t *testing.T) {
	for _, src := range []string{`-0`, `-0.0`, `0e0`, `-0e0`, `9007199254740991`, `9007199254740992`, `9007199254740993`, `9223372036854775807`, `-9223372036854775808`, `18446744073709551615`, `1e-4000`, `-1e-4000`} {
		var got any
		var want float64
		if err := json.Unmarshal([]byte(src), &want); err != nil {
			t.Fatal(err)
		}
		if err := UnmarshalDynamic([]byte(src), &got); err != nil {
			t.Fatal(err)
		}
		f, ok := got.(float64)
		if !ok || math.Float64bits(f) != math.Float64bits(want) {
			t.Fatalf("%s=%v (%T) bits=%x; want %v bits=%x", src, got, got, math.Float64bits(f), want, math.Float64bits(want))
		}
	}
	for _, src := range []string{`1e400`, `-1e400`, `01`, `1.`, `1e`, `1e+`, `1 2`, `[1,]`} {
		var got any
		if err := UnmarshalDynamic([]byte(src), &got); err == nil {
			t.Fatalf("accepted invalid/overflow number %s", src)
		}
	}
	var got any = json.Number("99")
	if err := UnmarshalDynamic([]byte(`{"bad":1e400,"after":2}`), &got); err == nil || got != nil {
		t.Fatalf("overflow should clear dynamic destination: %#v %v", got, err)
	}
}

func TestDynamicDuplicateAndStringSemantics(t *testing.T) {
	for _, src := range []string{`{"a":{"x":1},"a":{"y":2}}`, `{"a":1,"a":null}`, `{"a":[1],"a":[2]}`, `{"\u0061":1,"a":2}`, `null`, `[]`, `{}`, `"é😀"`, `"\ud800"`, `"a\n\"b"`, "\"\xff\"", `"\q"`} {
		var got, want any
		e1 := UnmarshalDynamic([]byte(src), &got)
		e2 := json.Unmarshal([]byte(src), &want)
		if (e1 == nil) != (e2 == nil) || e1 == nil && !dynamicEqual(got, want) {
			t.Fatalf("%q: got=%#v,%v want=%#v,%v", src, got, e1, want, e2)
		}
	}
	m := make(map[string]any)
	alias := m
	if err := UnmarshalDynamic([]byte(`{"a":1}`), &m); err != nil || alias["a"] != float64(1) {
		t.Fatalf("empty map identity lost: %#v %v", alias, err)
	}
	if err := UnmarshalDynamic([]byte(`null`), &m); err != nil || m != nil {
		t.Fatalf("map null=%#v %v", m, err)
	}
}

func TestDynamicPrefilledPointersUseStandardDecoder(t *testing.T) {
	custom := new(customNumber)
	var out any = custom
	if err := UnmarshalDynamic([]byte(`1`), &out); err != nil || out != custom || custom.calls != 1 || custom.Value != json.Number("91") {
		t.Fatalf("custom dispatch=%#v %v", out, err)
	}
	typed := new(struct{ Value int })
	out = typed
	if err := UnmarshalDynamic([]byte(`{"Value":3}`), &out); err != nil || out != typed || typed.Value != 3 {
		t.Fatalf("typed pointer=%#v %v", out, err)
	}
	var self any
	self = &self
	if err := UnmarshalDynamic([]byte(`1`), &self); err != nil || self != float64(1) {
		t.Fatalf("self interface=%#v %v", self, err)
	}
}

func TestDynamicExistingMapKeepsStandardDispatch(t *testing.T) {
	// Existing maps stay on the previous reflection fallback, including its
	// callback and merge behavior, instead of entering the new generic parser.
	custom1, custom2 := new(customNumber), new(customNumber)
	got := map[string]any{"x": custom1, "keep": json.Number("99")}
	want := map[string]any{"x": custom2, "keep": json.Number("99")}
	raw := []byte(`{"x":1,"new":2}`)
	e1 := UnmarshalDynamic(raw, &got)
	e2 := json.Unmarshal(raw, &want)
	if (e1 == nil) != (e2 == nil) || !reflect.DeepEqual(got, want) || custom1.calls != custom2.calls {
		t.Fatalf("changed existing map dispatch: got=%#v/%d/%v want=%#v/%d/%v", got, custom1.calls, e1, want, custom2.calls, e2)
	}
}

func FuzzDynamicUnmarshalAgreement(f *testing.F) {
	for _, src := range []string{`null`, `-0`, `1.5`, `1e400`, `{"a":1,"a":{"b":[1,true,null]}}`, `"\ud800"`, `{"\u0061":1,"a":2}`, "\"\xff\"", `{"a":}`, `[1,]`} {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		data := []byte(src)
		var got, want any
		e1 := UnmarshalDynamic(data, &got)
		e2 := json.Unmarshal(data, &want)
		if (e1 == nil) != (e2 == nil) || e1 == nil && !dynamicEqual(got, want) {
			t.Fatalf("any %q: got=%#v/%v want=%#v/%v", src, got, e1, want, e2)
		}
		var gm, wm map[string]any
		e1 = UnmarshalDynamic(data, &gm)
		e2 = json.Unmarshal(data, &wm)
		if (e1 == nil) != (e2 == nil) || e1 == nil && !dynamicEqual(gm, wm) {
			t.Fatalf("map %q: got=%#v/%v want=%#v/%v", src, gm, e1, wm, e2)
		}
	})
}

func dynamicEqual(a, b any) bool {
	switch a := a.(type) {
	case float64:
		b, ok := b.(float64)
		return ok && math.Float64bits(a) == math.Float64bits(b)
	case []any:
		b, ok := b.([]any)
		if !ok || len(a) != len(b) || (a == nil) != (b == nil) {
			return false
		}
		for i := range a {
			if !dynamicEqual(a[i], b[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		b, ok := b.(map[string]any)
		if !ok || len(a) != len(b) || (a == nil) != (b == nil) {
			return false
		}
		for k, v := range a {
			w, ok := b[k]
			if !ok || !dynamicEqual(v, w) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}
