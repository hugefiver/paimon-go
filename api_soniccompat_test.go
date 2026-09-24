//go:build !sonic_stdjson && !sonic_jsonv2

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
	if !Valid([]byte(`{"a":"\q","b":1}`)) {
		t.Fatalf(`Valid({"a":"\q","b":1}) = false, want true`)
	}
	n, err = Get([]byte(`{"a":{"b":"\q"}}`), "a")
	if err != nil {
		t.Fatalf(`Get({"a":{"b":"\q"}}, "a") error = %v, want nil`, err)
	}
	if raw, err := n.Raw(); err != nil || raw != `{"b":"\q"}` {
		t.Fatalf("Raw() = %q, %v; want raw invalid-escape container", raw, err)
	}
}

// These boundaries are witnessed against Sonic v1.15.2's native codec.
// Its Go 1.27 fallback used a different, more permissive number scanner.
func TestDefaultBuildMatchesSonicMalformedNumberBoundaries(t *testing.T) {
	for _, data := range []string{`{"a":01}`, `{"a":1.}`, `{"a":1e}`, `{"a":+1}`, "1e ", "1e,"} {
		if Valid([]byte(data)) {
			t.Fatalf("Valid(%q) = true", data)
		}
		if _, err := Get([]byte(data)); err == nil {
			t.Fatalf("Get(%q) accepted an invalid root", data)
		}
	}
	for _, data := range []string{`{"a":1.}`, `{"a":1e}`, `{"a":+1}`} {
		if _, err := Get([]byte(data), "a"); err == nil {
			t.Fatalf("Get(%q, a) accepted an invalid number", data)
		}
	}
}

func TestDefaultBuildStopsLeadingZeroNumberAtFirstValue(t *testing.T) {
	const data = `{"a":0123}`
	if Valid([]byte(data)) {
		t.Fatal("Valid accepted a leading-zero object member")
	}
	for name, fn := range map[string]func() (ast.Node, error){
		"Get":           func() (ast.Node, error) { return Get([]byte(data), "a") },
		"GetString":     func() (ast.Node, error) { return GetFromString(data, "a") },
		"GetCopyString": func() (ast.Node, error) { return GetCopyFromString(data, "a") },
		"Validate": func() (ast.Node, error) {
			return GetWithOptions([]byte(data), ast.SearchOptions{ValidateJSON: true}, "a")
		},
		"Root": func() (ast.Node, error) { return Get([]byte(`0123`)) },
	} {
		t.Run(name, func(t *testing.T) {
			n, err := fn()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := n.Raw()
			if err != nil || raw != "0" {
				t.Fatalf("raw=%q err=%v, want 0", raw, err)
			}
		})
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
