package fastjsoncompat

import (
	"testing"

	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/internal/compatmode"
)

func TestValidSonicCompatibilityBoundaries(t *testing.T) {
	for _, input := range [][]byte{
		[]byte(`{"a":01}`),
		[]byte(`{"a":"\q","b":1}`),
	} {
		if Valid(input) {
			t.Fatalf("Valid(%q) = true, want false", input)
		}
		if ValidString(string(input)) {
			t.Fatalf("ValidString(%q) = true, want false", input)
		}
	}

	rawControl := []byte{'{', '"', 'a', '"', ':', '"', 0x11, 'x', '"', '}'}
	if got, want := Valid(rawControl), !compatmode.StdJSON; got != want {
		t.Fatalf("Valid(raw control) = %v, want %v", got, want)
	}
	if got, want := ValidString(string(rawControl)), !compatmode.StdJSON; got != want {
		t.Fatalf("ValidString(raw control) = %v, want %v", got, want)
	}
	invalidUTF8 := []byte{'{', '"', 'a', '"', ':', '"', 0xff, '"', '}'}
	if !Valid(invalidUTF8) {
		t.Fatal("Valid(invalid UTF-8 string) = false, want true")
	}
	if !ValidString(string(invalidUTF8)) {
		t.Fatal("ValidString(invalid UTF-8 string) = false, want true")
	}
}

func TestGetValidatedBareExponentCompatibility(t *testing.T) {
	input := []byte(`{"a":{"b":1e}}`)
	node, err := Get(input, ast.SearchOptions{ValidateJSON: true}, "a")
	if compatmode.StdJSON {
		if err == nil {
			t.Fatal("strict Get(bare exponent) error = nil, want error")
		}
		return
	}
	if err != nil {
		t.Fatalf("Get(bare exponent) error = %v", err)
	}
	raw, err := node.Raw()
	if err != nil || raw != `{"b":1e}` {
		t.Fatalf("Get(bare exponent).Raw() = %q, %v; want raw container", raw, err)
	}
}

func TestGetSkipsOnlyBalancedPreTargetContainers(t *testing.T) {
	if compatmode.StdJSON {
		t.Skip("pre-target malformed-container skipping is Sonic compatibility behavior")
	}

	for _, tt := range []struct {
		name  string
		input string
		path  []interface{}
		want  string
	}{
		{name: "object", input: `{"broken":{garbage},"a":1}`, path: []interface{}{"a"}, want: "1"},
		{name: "array", input: `[{garbage},1]`, path: []interface{}{1}, want: "1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			node, err := Get([]byte(tt.input), ast.SearchOptions{}, tt.path...)
			if err != nil {
				t.Fatalf("Get(%q, %v) error = %v, want success", tt.input, tt.path, err)
			}
			raw, err := node.Raw()
			if err != nil || raw != tt.want {
				t.Fatalf("Get(%q, %v).Raw() = %q, %v; want %q, nil", tt.input, tt.path, raw, err, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name  string
		input string
		path  []interface{}
	}{
		{name: "selected object container", input: `{"a":{garbage}}`, path: []interface{}{"a"}},
		{name: "selected array container", input: `[{garbage}]`, path: []interface{}{0}},
		{name: "prior object scalar", input: `{"broken":garbage,"a":1}`, path: []interface{}{"a"}},
		{name: "prior array scalar", input: `[garbage,1]`, path: []interface{}{1}},
		{name: "prior object unclosed container", input: `{"broken":{garbage,"a":1}`, path: []interface{}{"a"}},
		{name: "prior array unclosed container", input: `[{garbage,1]`, path: []interface{}{1}},
		{name: "prior object mismatched container", input: `{"broken":{[garbage},"a":1}`, path: []interface{}{"a"}},
		{name: "prior array mismatched container", input: `[{[garbage},1]`, path: []interface{}{1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Get([]byte(tt.input), ast.SearchOptions{}, tt.path...); err == nil {
				t.Fatalf("Get(%q, %v) error = nil, want malformed JSON error", tt.input, tt.path)
			}
		})
	}
}

func TestGetRoutesExplicitSearcherOptionsWithoutChangingDefault(t *testing.T) {
	if compatmode.StdJSON {
		t.Skip("explicit Searcher routing is Sonic compatibility behavior")
	}

	const malformedSelected = `{"selected":{garbage}}`
	if _, err := Get([]byte(malformedSelected), ast.SearchOptions{}, "selected"); err == nil {
		t.Fatal("default all-false Get accepted malformed selected container")
	}

	for _, opts := range []ast.SearchOptions{
		{CopyReturn: true},
		{ConcurrentRead: true},
	} {
		node, err := Get([]byte(malformedSelected), opts, "selected")
		if err != nil {
			t.Fatalf("Get(%+v) error = %v", opts, err)
		}
		raw, err := node.Raw()
		if err != nil || raw != `{garbage}` {
			t.Fatalf("Get(%+v) Raw() = %q, %v; want {garbage}, nil", opts, raw, err)
		}
	}

	root, err := Get([]byte(`{garbage} trailing`), ast.SearchOptions{CopyReturn: true})
	if err != nil {
		t.Fatalf("root CopyReturn Get error = %v", err)
	}
	if raw, err := root.Raw(); err != nil || raw != `{garbage}` {
		t.Fatalf("root CopyReturn Get Raw() = %q, %v; want {garbage}, nil", raw, err)
	}

	for _, input := range []string{
		`{"before":{garbage},"selected":1}`,
		`{"selected":1,"after":{garbage}}`,
	} {
		node, err := Get([]byte(input), ast.SearchOptions{ValidateJSON: true}, "selected")
		if err != nil {
			t.Fatalf("ValidateJSON Get(%q) error = %v", input, err)
		}
		raw, err := node.Raw()
		if err != nil || raw != "1" {
			t.Fatalf("ValidateJSON Get(%q) Raw() = %q, %v; want 1, nil", input, raw, err)
		}
	}
}

func TestSearcherRoutingPredicates(t *testing.T) {
	for _, tt := range []struct {
		name       string
		opts       ast.SearchOptions
		searcher   bool
		strictPath bool
	}{
		{name: "default"},
		{name: "validate", opts: ast.SearchOptions{ValidateJSON: true}, searcher: true},
		{name: "copy", opts: ast.SearchOptions{CopyReturn: true}, searcher: true, strictPath: true},
		{name: "concurrent", opts: ast.SearchOptions{ConcurrentRead: true}, searcher: true, strictPath: true},
		{name: "all", opts: ast.SearchOptions{ValidateJSON: true, CopyReturn: true, ConcurrentRead: true}, searcher: true, strictPath: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUseSearcher(tt.opts); got != tt.searcher {
				t.Fatalf("shouldUseSearcher(%+v) = %t, want %t", tt.opts, got, tt.searcher)
			}
			if got := shouldUseStrictSearcher(tt.opts); got != tt.strictPath {
				t.Fatalf("shouldUseStrictSearcher(%+v) = %t, want %t", tt.opts, got, tt.strictPath)
			}
		})
	}
}

func TestDefaultRootGetPreservesSearcherValidation(t *testing.T) {
	if compatmode.StdJSON {
		t.Skip("default root Searcher behavior is Sonic compatibility behavior")
	}

	for _, input := range []string{`{garbage}`, `{"a":{garbage}}`} {
		if _, err := Get([]byte(input), ast.SearchOptions{}); err == nil {
			t.Fatalf("default root Get(%q) error = nil, want malformed JSON error", input)
		}
	}

	for _, input := range []string{`"\q"`, `{"a":"\q","b":1}`} {
		node, err := Get([]byte(input), ast.SearchOptions{})
		if err != nil {
			t.Fatalf("default root Get(%q) error = %v", input, err)
		}
		raw, err := node.Raw()
		if err != nil || raw != input {
			t.Fatalf("default root Get(%q).Raw() = %q, %v; want original raw", input, raw, err)
		}
	}

	node, err := Get([]byte(`{"before":{garbage},"selected":1}`), ast.SearchOptions{}, "selected")
	if err != nil {
		t.Fatalf("default nonempty Get error = %v", err)
	}
	if raw, err := node.Raw(); err != nil || raw != "1" {
		t.Fatalf("default nonempty Get Raw() = %q, %v; want 1, nil", raw, err)
	}
}

func TestScanContainerEndSkipsContentsWithinDepthLimit(t *testing.T) {
	data := []byte(`{"string":"}","escaped":"\\\"}","nested":[garbage]}`)
	if end, ok := scanContainerEnd(data, 0); !ok || end != len(data) {
		t.Fatalf("scanContainerEnd(%q) = %d, %t; want %d, true", data, end, ok, len(data))
	}

	for _, tt := range []struct {
		name  string
		depth int
		ok    bool
	}{
		{name: "maximum", depth: maxScanDepth, ok: true},
		{name: "too deep", depth: maxScanDepth + 1, ok: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, 0, tt.depth*2+len("garbage"))
			for i := 0; i < tt.depth; i++ {
				data = append(data, '[')
			}
			data = append(data, "garbage"...)
			for i := 0; i < tt.depth; i++ {
				data = append(data, ']')
			}

			end, ok := scanContainerEnd(data, 0)
			if ok != tt.ok || (ok && end != len(data)) {
				t.Fatalf("scanContainerEnd(depth %d) = %d, %t; want end %d, %t", tt.depth, end, ok, len(data), tt.ok)
			}
		})
	}
}
