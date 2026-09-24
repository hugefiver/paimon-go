package ast

import (
	"testing"
)

func TestNativeRejectsBareExponent(t *testing.T) {
	for _, input := range []string{"1e", "1e ", "1e,", "1e+", "1e-", "1.", `[1e]`, `{"a":{"b":1e}}`} {
		if got := NewRaw(input).Type(); got != V_ERROR {
			t.Fatalf("NewRaw(%q).Type = %d", input, got)
		}
		if _, err := NewSearcher(input).GetByPath(); err == nil {
			t.Fatalf("Searcher(%q) succeeded", input)
		}
	}
	if _, err := NewSearcher(`{"a":{"b":1e}}`).GetByPath("a", "b"); err == nil {
		t.Fatal("selected bare exponent accepted")
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

func TestPreorderNativeRejectsBareExponentInBothNumberModes(t *testing.T) {
	for _, only := range []bool{false, true} {
		if err := Preorder(`[1e]`, &recordingVisitor{}, &VisitorOptions{OnlyNumber: only}); err == nil {
			t.Fatalf("Preorder accepted bare exponent, OnlyNumber=%v", only)
		}
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
