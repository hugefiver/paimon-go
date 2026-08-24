//go:build !sonic_stdjson

package sonic

import (
	"strings"
	"testing"

	"github.com/bytedance/sonic/ast"
)

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
	if Valid([]byte(`{"a":"\q","b":1}`)) {
		t.Fatalf(`Valid({"a":"\q","b":1}) = true, want false`)
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
		[]byte(`{"a":1.}`),
		[]byte(`{"a":1e}`),
		[]byte(`{"a":+1}`),
		[]byte{'{', '"', 'a', '"', ':', '"', 0x11, 'a', '"', ',', '"', 'b', '"', ':', '1', 'e', '}'},
	} {
		if Valid(data) {
			t.Fatalf("Valid(%q) = true, want false", data)
		}
	}

	for _, data := range [][]byte{
		[]byte(`{"a":1.}`),
		[]byte(`{"a":+1}`),
	} {
		if _, err := Get(data); err == nil {
			t.Fatalf("Get(%q) error = nil, want error", data)
		}
	}
	for _, data := range [][]byte{
		[]byte(`{"a":1e}`),
		[]byte{'{', '"', 'a', '"', ':', '"', 0x11, 'a', '"', ',', '"', 'b', '"', ':', '1', 'e', '}'},
	} {
		n, err := Get(data)
		if err != nil {
			t.Fatalf("Get(%q) error = %v, want tolerant raw container", data, err)
		}
		if raw, err := n.Raw(); err != nil || raw != string(data) {
			t.Fatalf("Get(%q).Raw() = %q, %v; want original raw", data, raw, err)
		}
	}

	n, err := Get([]byte(`{"a":01}`), "a")
	if err != nil {
		t.Fatalf(`Get({"a":01}, "a") error = %v, want nil`, err)
	}
	raw, err := n.Raw()
	if err != nil || raw != "01" {
		t.Fatalf(`Get({"a":01}, "a").Raw() = %q, %v; want "01", nil`, raw, err)
	}

	for _, data := range [][]byte{
		[]byte(`{"a":1.}`),
		[]byte(`{"a":+1}`),
	} {
		if _, err := Get(data, "a"); err == nil {
			t.Fatalf("Get(%q, a) error = nil, want error", data)
		}
	}
	n, err = Get([]byte(`{"a":1e}`), "a")
	if err != nil {
		t.Fatalf(`Get({"a":1e}, a) error = %v, want nil`, err)
	}
	if raw, err := n.Raw(); err != nil || raw != "1e" {
		t.Fatalf(`Get({"a":1e}, a).Raw() = %q, %v; want "1e", nil`, raw, err)
	}
}

func TestDefaultBuildPreservesLeadingZeroNumberLiteralsLikeSonic(t *testing.T) {
	data := []byte(`{"a":0123}`)
	if Valid(data) {
		t.Fatalf(`Valid({"a":0123}) = true, want false`)
	}
	for name, fn := range map[string]func() (ast.Node, error){
		"Get": func() (ast.Node, error) { return Get(data, "a") },
		"GetWithOptionsValidate": func() (ast.Node, error) {
			return GetWithOptions(data, ast.SearchOptions{ValidateJSON: true}, "a")
		},
	} {
		n, err := fn()
		if err != nil {
			t.Fatalf("%s leading-zero error = %v", name, err)
		}
		raw, err := n.Raw()
		if err != nil || raw != "0123" {
			t.Fatalf("%s leading-zero Raw() = %q, %v; want 0123, nil", name, raw, err)
		}
	}
	n, err := Get([]byte(`0123`))
	if err != nil {
		t.Fatalf("Get(0123) error = %v", err)
	}
	if raw, err := n.Raw(); err != nil || raw != "0123" {
		t.Fatalf("Get(0123).Raw() = %q, %v; want 0123, nil", raw, err)
	}
}

func TestDefaultBuildValidateJSONAllowsDeepNestingUpTo4096(t *testing.T) {
	deep := strings.Repeat("[", 400) + "0" + strings.Repeat("]", 400)
	n, err := GetWithOptions([]byte(`{"a":`+deep+`}`), ast.SearchOptions{ValidateJSON: true}, "a")
	if err != nil {
		t.Fatalf("GetWithOptions ValidateJSON 400-deep error = %v", err)
	}
	raw, err := n.Raw()
	if err != nil {
		t.Fatalf("deep Raw() error = %v", err)
	}
	if raw != deep {
		t.Fatalf("deep Raw() length = %d; want %d", len(raw), len(deep))
	}
}
