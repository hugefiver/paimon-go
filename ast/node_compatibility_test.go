package ast

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNodeCompatibilityAnyMutatorsKeepVAny(t *testing.T) {
	t.Run("AddAny", func(t *testing.T) {
		var node Node
		if err := node.AddAny(1); err != nil {
			t.Fatalf("AddAny() error = %v", err)
		}
		if child := node.Index(0); child == nil || child.Type() != V_ANY {
			t.Fatalf("AddAny() child type = %v; want V_ANY", child)
		}
	})

	t.Run("SetAny", func(t *testing.T) {
		var node Node
		if overwritten, err := node.SetAny("value", 1); err != nil || overwritten {
			t.Fatalf("SetAny() = %v, %v; want false, nil", overwritten, err)
		}
		if child := node.Get("value"); child == nil || child.Type() != V_ANY {
			t.Fatalf("SetAny() child type = %v; want V_ANY", child)
		}
	})

	t.Run("SetAnyByIndex", func(t *testing.T) {
		var node Node
		if overwritten, err := node.SetAnyByIndex(0, 1); err != nil || overwritten {
			t.Fatalf("SetAnyByIndex() = %v, %v; want false, nil", overwritten, err)
		}
		if child := node.Index(0); child == nil || child.Type() != V_ANY {
			t.Fatalf("SetAnyByIndex() child type = %v; want V_ANY", child)
		}
	})
}

func TestNodeCompatibilityMutationEdges(t *testing.T) {
	for _, node := range []*Node{
		ptr(NewArray([]Node{NewNumber("1")})),
		ptr(NewObject([]Pair{NewPair("value", NewNumber("1"))})),
	} {
		for _, index := range []int{-1, 1} {
			if removed, err := node.UnsetByIndex(index); removed || !errors.Is(err, ErrNotExist) {
				t.Fatalf("UnsetByIndex(%d) = %v, %v; want false, ErrNotExist", index, removed, err)
			}
		}
	}

	for _, node := range []*Node{ptr(NewArray(nil)), ptr(NewObject(nil))} {
		if err := node.Pop(); err != nil {
			t.Fatalf("Pop(empty %d) error = %v; want nil", node.Type(), err)
		}
	}
}

func TestNodeCompatibilitySortKeysFalseVisitsArrayChildren(t *testing.T) {
	makeNode := func() Node {
		return NewArray([]Node{
			NewObject([]Pair{
				NewPair("b", NewNumber("2")),
				NewPair("a", NewNumber("1")),
			}),
			NewArray([]Node{NewObject([]Pair{
				NewPair("d", NewNumber("4")),
				NewPair("c", NewNumber("3")),
			})}),
		})
	}

	t.Run("false crosses nested arrays to sort object containers", func(t *testing.T) {
		node := makeNode()
		if err := node.SortKeys(false); err != nil {
			t.Fatalf("SortKeys(false) error = %v", err)
		}
		if raw, err := node.Raw(); err != nil || raw != `[{"a":1,"b":2},[{"c":3,"d":4}]]` {
			t.Fatalf("SortKeys(false) raw = %q, %v; want nested-array object sort", raw, err)
		}
	})

	t.Run("true descends below immediate child", func(t *testing.T) {
		node := makeNode()
		if err := node.SortKeys(true); err != nil {
			t.Fatalf("SortKeys(true) error = %v", err)
		}
		if raw, err := node.Raw(); err != nil || raw != `[{"a":1,"b":2},[{"c":3,"d":4}]]` {
			t.Fatalf("SortKeys(true) raw = %q, %v; want recursive sort", raw, err)
		}
	})
}

func TestNodeCompatibilitySortKeysMaterializesLazyContainers(t *testing.T) {
	for _, tt := range []struct {
		name    string
		node    Node
		recurse bool
		want    string
	}{
		{
			name:    "false lazy object child",
			node:    NewArray([]Node{NewRaw(`{"b":2,"a":1}`)}),
			want:    `[{"a":1,"b":2}]`,
			recurse: false,
		},
		{
			name:    "false lazy nested array child",
			node:    NewArray([]Node{NewRaw(`[{"d":4,"c":3}]`)}),
			want:    `[[{"c":3,"d":4}]]`,
			recurse: false,
		},
		{
			name: "true lazy object descendant",
			node: NewObject([]Pair{
				NewPair("outer", NewRaw(`{"d":4,"c":3}`)),
			}),
			want:    `{"outer":{"c":3,"d":4}}`,
			recurse: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.node.SortKeys(tt.recurse); err != nil {
				t.Fatalf("SortKeys(%t) error = %v", tt.recurse, err)
			}
			if got, err := tt.node.Raw(); err != nil || got != tt.want {
				t.Fatalf("SortKeys(%t) raw = %q, %v; want %q, nil", tt.recurse, got, err, tt.want)
			}
		})
	}
}

func TestNodeCompatibilityIndexOrGetWithIdxIsObjectOnly(t *testing.T) {
	object := NewObject([]Pair{
		NewPair("first", NewNumber("1")),
		NewPair("second", NewNumber("2")),
	})
	child, idx := object.IndexOrGetWithIdx(0, "second")
	if child == nil || idx != 1 {
		t.Fatalf("object IndexOrGetWithIdx() = %v, %d; want second child, 1", child, idx)
	}
	if got, err := child.Int64(); err != nil || got != 2 {
		t.Fatalf("resolved child = %d, %v; want 2, nil", got, err)
	}

	for _, node := range []Node{NewArray([]Node{NewNumber("1")}), NewString("sonic")} {
		child, idx = node.IndexOrGetWithIdx(7, "ignored")
		if child == nil || child.Type() != V_ERROR || idx != 7 {
			t.Fatalf("%d IndexOrGetWithIdx() = %v, %d; want V_ERROR, 7", node.Type(), child, idx)
		}
		if err := child.Check(); !errors.Is(err, ErrUnsupportType) {
			t.Fatalf("%d IndexOrGetWithIdx() error = %v; want ErrUnsupportType", node.Type(), err)
		}
	}
}

func TestNodeCompatibilityZeroValueSemantics(t *testing.T) {
	var zero Node
	if length, err := zero.Len(); err != nil || length != 0 {
		t.Fatalf("zero Len() = %d, %v; want 0, nil", length, err)
	}
	if data, err := zero.MarshalJSON(); !errors.Is(err, ErrNotExist) || data != nil {
		t.Fatalf("zero MarshalJSON() = %q, %v; want nil, ErrNotExist", data, err)
	}

	calls := 0
	if err := zero.ForEach(func(sequence Sequence, child *Node) bool {
		calls++
		if sequence.Index != -1 || child == nil || child.Type() != V_NONE {
			t.Fatalf("zero ForEach callback = %v, %v; want Index -1, V_NONE", sequence, child)
		}
		return true
	}); err != nil {
		t.Fatalf("zero ForEach() error = %v; want nil", err)
	}
	if calls != 1 {
		t.Fatalf("zero ForEach() calls = %d; want 1", calls)
	}

	var nilNode *Node
	if length, err := nilNode.Len(); !errors.Is(err, ErrNotExist) || length != 0 {
		t.Fatalf("nil Len() = %d, %v; want 0, ErrNotExist", length, err)
	}
	if data, err := nilNode.MarshalJSON(); !errors.Is(err, ErrNotExist) || data != nil {
		t.Fatalf("nil MarshalJSON() = %q, %v; want nil, ErrNotExist", data, err)
	}
}

func TestNodeCompatibilityRetainsSaferExtensions(t *testing.T) {
	t.Run("malformed UnmarshalJSON stays error node", func(t *testing.T) {
		var node Node
		if err := node.UnmarshalJSON([]byte("{")); err != nil {
			t.Fatalf("UnmarshalJSON() error = %v", err)
		}
		if node.Type() != V_ERROR {
			t.Fatalf("UnmarshalJSON({).Type() = %d; want V_ERROR", node.Type())
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("MarshalJSON() panicked: %v", recovered)
			}
		}()
		if data, err := node.MarshalJSON(); data != nil || err == nil {
			t.Fatalf("MarshalJSON() = %q, %v; want nil, error", data, err)
		}
	})

	t.Run("invalid UTF-8 string marshals valid JSON", func(t *testing.T) {
		node := NewString(string([]byte{'a', 0xff, 'b'}))
		data, err := node.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON() error = %v", err)
		}
		if !json.Valid(data) {
			t.Fatalf("MarshalJSON() = %q; want valid JSON", data)
		}
	})

	t.Run("malformed Parser container remains deferred", func(t *testing.T) {
		node, code := NewParser("{").Parse()
		if code != 0 || node.Type() != V_OBJECT {
			t.Fatalf("Parse({) = type %d, code %v; want V_OBJECT, 0", node.Type(), code)
		}
		if err := node.LoadAll(); err == nil {
			t.Fatal("Parse({).LoadAll() error = nil; want deferred error")
		}
	})
}

func ptr(node Node) *Node { return &node }
