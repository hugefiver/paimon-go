package ast

import (
	"reflect"
	"testing"
)

func TestBareExponentRawCompatibility(t *testing.T) {
	const input = `{"a":{"b":1e}}`

	parsed, code := NewParser(input).Parse()
	if code != 0 {
		t.Fatalf("Parse(%q) code = %v, want success", input, code)
	}
	assertNodeRaw(t, parsed.Get("a").Get("b"), "1e")

	raw := NewRaw(input)
	if got := raw.Type(); got != V_OBJECT {
		t.Fatalf("NewRaw(%q).Type() = %d, want V_OBJECT", input, got)
	}
	if err := raw.LoadAll(); err != nil {
		t.Fatalf("NewRaw(%q).LoadAll() error = %v", input, err)
	}
	assertNodeRaw(t, raw.Get("a").Get("b"), "1e")

	got, err := NewSearcher(input).GetByPath("a", "b")
	if err != nil {
		t.Fatalf("Searcher.GetByPath(a, b) error = %v", err)
	}
	assertNodeRaw(t, &got, "1e")

	if _, code := NewParser(`1e`).Parse(); code == 0 {
		t.Fatal("Parse(1e) code = success, want the upstream root-EOF rejection")
	}
	if _, err := NewSearcher(`1e`).GetByPath(); err == nil {
		t.Fatal("Searcher.GetByPath() for root 1e error = nil, want error")
	}
	if got := NewRaw(`1e`).Type(); got != V_ERROR {
		t.Fatalf("NewRaw(1e).Type() = %d, want V_ERROR", got)
	}
}

func TestRootBareExponentDelimiterCompatibility(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
	}{
		{name: "space", input: "1e "},
		{name: "comma", input: "1e,"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := NewRaw(tt.input)
			if got := raw.Type(); got != V_NUMBER {
				t.Fatalf("NewRaw(%q).Type() = %d, want V_NUMBER", tt.input, got)
			}
			assertNodeRaw(t, &raw, "1e")

			got, err := NewSearcher(tt.input).GetByPath()
			if err != nil {
				t.Fatalf("GetByPath(%q) error = %v, want success", tt.input, err)
			}
			assertNodeRaw(t, &got, "1e")
		})
	}

	for _, input := range []string{"1e", "1e+", "1e-", "1."} {
		t.Run("reject_"+input, func(t *testing.T) {
			if got := NewRaw(input).Type(); got != V_ERROR {
				t.Fatalf("NewRaw(%q).Type() = %d, want V_ERROR", input, got)
			}
			if _, err := NewSearcher(input).GetByPath(); err == nil {
				t.Fatalf("GetByPath(%q) error = nil, want error", input)
			}
		})
	}
}

func TestSearcherNonEmptyPathStopsAfterMatchedValue(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
		path  []interface{}
		want  string
	}{
		{name: "object sibling after b", input: `{"b":2,"a":{`, path: []interface{}{"b"}, want: "2"},
		{name: "object sibling after a", input: `{"a":1,"b":}`, path: []interface{}{"a"}, want: "1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewSearcher(tt.input).GetByPath(tt.path...)
			if err != nil {
				t.Fatalf("GetByPath(%q, %v) error = %v, want success", tt.input, tt.path, err)
			}
			assertNodeRaw(t, &got, tt.want)
		})
	}

	for _, tt := range []struct {
		name  string
		input string
		path  []interface{}
	}{
		{name: "selected value is malformed", input: `{"a":{`, path: []interface{}{"a"}},
		{name: "malformed member before target", input: `{"broken":{,"a":1}`, path: []interface{}{"a"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewSearcher(tt.input).GetByPath(tt.path...); err == nil {
				t.Fatalf("GetByPath(%q, %v) error = nil, want malformed JSON error", tt.input, tt.path)
			}
		})
	}
}

func TestSearcherValidationMatchesSonicFirstValueBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
		path  []interface{}
		want  string
	}{
		{name: "trailing data", input: `{"a":1}x`, path: []interface{}{"a"}, want: "1"},
		{name: "invalid escape", input: `{"a":"\q","b":1}`, path: []interface{}{"b"}, want: "1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewSearcher(tt.input).GetByPath(tt.path...)
			if err != nil {
				t.Fatalf("GetByPath(%q) error = %v", tt.input, err)
			}
			assertNodeRaw(t, &got, tt.want)
		})
	}

	for _, input := range []string{`{"a":1.}`, `{"a":1e+}`, `{"a":1e-}`} {
		if _, err := NewSearcher(input).GetByPath("a"); err == nil {
			t.Fatalf("GetByPath(%q) error = nil, want malformed-number error", input)
		}
	}
}

func TestSearcherSkipsOnlyBalancedPreTargetContainers(t *testing.T) {
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
			got, err := NewSearcher(tt.input).GetByPath(tt.path...)
			if err != nil {
				t.Fatalf("GetByPath(%q, %v) error = %v, want success", tt.input, tt.path, err)
			}
			assertNodeRaw(t, &got, tt.want)
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
			if _, err := NewSearcher(tt.input).GetByPath(tt.path...); err == nil {
				t.Fatalf("GetByPath(%q, %v) error = nil, want malformed JSON error", tt.input, tt.path)
			}
		})
	}
}

func TestPreorderOnlyNumberAcceptsBareExponentInContainer(t *testing.T) {
	visitor := &recordingVisitor{}
	if err := Preorder(`[1e]`, visitor, &VisitorOptions{OnlyNumber: true}); err != nil {
		t.Fatalf("Preorder([1e], OnlyNumber) error = %v", err)
	}
	if want := []string{"array", "float:1e", "array-end"}; !reflect.DeepEqual(visitor.events, want) {
		t.Fatalf("Preorder([1e], OnlyNumber) events = %#v, want %#v", visitor.events, want)
	}
	if len(visitor.floatCalls) != 1 || visitor.floatCalls[0].value != 0 {
		t.Fatalf("Preorder([1e], OnlyNumber) float calls = %#v, want one zero-valued call", visitor.floatCalls)
	}
	if err := Preorder(`[1e]`, &recordingVisitor{}, nil); err == nil {
		t.Fatal("Preorder([1e]) error = nil without OnlyNumber, want error")
	}
}

func assertNodeRaw(t *testing.T, node *Node, want string) {
	t.Helper()
	got, err := node.Raw()
	if err != nil {
		t.Fatalf("Node.Raw() error = %v", err)
	}
	if got != want {
		t.Fatalf("Node.Raw() = %q, want %q", got, want)
	}
}
