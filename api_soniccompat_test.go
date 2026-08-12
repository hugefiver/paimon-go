//go:build !sonic_stdjson

package sonic

import "testing"

func TestDefaultBuildEnablesObservedRawParserCompatibility(t *testing.T) {
	rawControl := []byte{'{', '"', 0x11, 'x', '"', ':', '1', '}'}
	if !Valid(rawControl) {
		t.Fatalf("Valid(raw control string) = false, want true by default")
	}
	var out map[string]interface{}
	if err := Unmarshal(rawControl, &out); err != nil {
		t.Fatalf("Unmarshal(raw control string) error = %v, want nil by default", err)
	}
	n, err := Get([]byte(`[1,true]x"",`))
	if err != nil {
		t.Fatalf("Get with trailing garbage error = %v, want nil by default", err)
	}
	raw, err := n.Raw()
	if err != nil || raw != `[1,true]` {
		t.Fatalf("Raw() = %q, %v; want first value", raw, err)
	}
	n, err = Get([]byte("7\xae3"))
	if err != nil {
		t.Fatalf("Get(number with trailing garbage) error = %v, want nil", err)
	}
	if raw, err := n.Raw(); err != nil || raw != "7" {
		t.Fatalf("Raw() = %q, %v; want first number", raw, err)
	}
	if !Valid([]byte(`{"a":"\q","b":1}`)) {
		t.Fatalf(`Valid({"a":"\q","b":1}) = false, want true by default`)
	}
	n, err = Get([]byte(`{"a":{"b":"\q"}}`), "a")
	if err != nil {
		t.Fatalf(`Get({"a":{"b":"\q"}}, "a") error = %v, want nil`, err)
	}
	if raw, err := n.Raw(); err != nil || raw != `{"b":"\q"}` {
		t.Fatalf("Raw() = %q, %v; want raw invalid-escape container", raw, err)
	}
}

func TestDefaultBuildMatchesSonicMalformedNumberBoundaries(t *testing.T) {
	for _, data := range [][]byte{
		[]byte(`{"a":01}`),
		[]byte(`{"a":1.}`),
		[]byte(`{"a":1e}`),
		[]byte(`{"a":+1}`),
		[]byte{'{', '"', 'a', '"', ':', '"', 0x11, 'a', '"', ',', '"', 'b', '"', ':', '0', '1', '}'},
		[]byte{'{', '"', 'a', '"', ':', '"', 0x11, 'a', '"', ',', '"', 'b', '"', ':', '1', 'e', '}'},
	} {
		if Valid(data) {
			t.Fatalf("Valid(%q) = true, want false", data)
		}
		if _, err := Get(data); err == nil {
			t.Fatalf("Get(%q) error = nil, want error", data)
		}
	}

	n, err := Get([]byte(`{"a":01}`), "a")
	if err != nil {
		t.Fatalf(`Get({"a":01}, "a") error = %v, want nil`, err)
	}
	raw, err := n.Raw()
	if err != nil || raw != "0" {
		t.Fatalf(`Get({"a":01}, "a").Raw() = %q, %v; want "0", nil`, raw, err)
	}

	for _, data := range [][]byte{
		[]byte(`{"a":1.}`),
		[]byte(`{"a":1e}`),
		[]byte(`{"a":+1}`),
	} {
		if _, err := Get(data, "a"); err == nil {
			t.Fatalf("Get(%q, a) error = nil, want error", data)
		}
	}
}
