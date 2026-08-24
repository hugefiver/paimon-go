package ast

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	nativetypes "github.com/bytedance/sonic/internal/native/types"
)

// Regression tests for issues found in the full-codebase review.

// SetByIndex on an object with a negative index must not panic (it
// previously indexed n.obj[-1]); Sonic returns an error instead.
func TestSetByIndexNegativeIndexOnObjectDoesNotPanic(t *testing.T) {
	n := NewObject([]Pair{NewPair("a", NewNumber("1"))})
	ok, err := n.SetByIndex(-1, NewNumber("9"))
	if err == nil || ok {
		t.Fatalf("SetByIndex(-1) = %v, %v; want false, error", ok, err)
	}
}

// Invalid UTF-8 lead bytes must not swallow a following quote or
// backslash: the encoded output must stay parseable JSON.
func TestStringNodeInvalidUTF8DoesNotSwallowDelimiter(t *testing.T) {
	n := NewString("a\xc3\"b")
	raw, err := n.Raw()
	if err != nil {
		t.Fatalf("Raw() error = %v", err)
	}
	if !json.Valid([]byte(raw)) {
		t.Fatalf("Raw() = %q is not valid JSON", raw)
	}
}

// Leading-zero numbers must round-trip verbatim (Sonic keeps the full
// literal) instead of being silently truncated to "0".
func TestLeadingZeroNumberLiteralsPreserved(t *testing.T) {
	n, code := NewParser("[0123]").Parse()
	if code != 0 {
		t.Fatalf("Parse([0123]) code = %v", code)
	}
	got, err := n.Index(0).Raw()
	if err != nil {
		t.Fatalf("Raw() error = %v", err)
	}
	if got != "0123" {
		t.Fatalf("leading-zero literal = %q; want 0123", got)
	}

	rawNode := NewRaw("0123")
	got, err = rawNode.Raw()
	if err != nil {
		t.Fatalf("NewRaw(0123).Raw() error = %v", err)
	}
	if got != "0123" {
		t.Fatalf("NewRaw(0123).Raw() = %q; want 0123", got)
	}

	s := NewSearcher(`{"a":0123}`)
	child, err := s.GetByPath("a")
	if err != nil {
		t.Fatalf("Searcher.GetByPath leading-zero error = %v", err)
	}
	got, err = child.Raw()
	if err != nil {
		t.Fatalf("Searcher child Raw() error = %v", err)
	}
	if got != "0123" {
		t.Fatalf("Searcher leading-zero Raw() = %q; want 0123", got)
	}

	sNoValidate := NewSearcher(`{"a":0123}`)
	sNoValidate.ValidateJSON = false
	child, err = sNoValidate.GetByPath("a")
	if err != nil {
		t.Fatalf("Searcher(no validate).GetByPath leading-zero error = %v", err)
	}
	got, err = child.Raw()
	if err != nil {
		t.Fatalf("Searcher(no validate) child Raw() error = %v", err)
	}
	if got != "0123" {
		t.Fatalf("Searcher(no validate) leading-zero Raw() = %q; want 0123", got)
	}

	var sb strings.Builder
	if err := Preorder(`[0123]`, &collectVisitor{sb: &sb}, nil); err != nil {
		t.Fatalf("Preorder([0123]) error = %v", err)
	}
	if !strings.Contains(sb.String(), "int:0123;") {
		t.Fatalf("Preorder([0123]) output = %q; want int callback with raw 0123", sb.String())
	}
}

// An unpaired \uD800 surrogate decodes to U+FFFD (Sonic semantics),
// both as a value and as an object key that stays addressable.
func TestUnpairedSurrogateBecomesReplacementChar(t *testing.T) {
	n, code := NewParser(`"\ud800"`).Parse()
	if code != 0 {
		t.Fatalf("Parse(\"\\ud800\") code = %v", code)
	}
	s, err := n.String()
	if err != nil {
		t.Fatalf("String() error = %v", err)
	}
	if s != "\ufffd" {
		t.Fatalf("unpaired surrogate = %q; want \\ufffd", s)
	}

	root := NewRaw(`{"\ud800":1}`)
	if err := root.LoadAll(); err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	child := root.Get("\ufffd")
	if !child.Exists() {
		t.Fatalf("object key with unpaired surrogate is not addressable")
	}
}

// Preorder must not swallow the second escape when a high surrogate is
// not followed by a low surrogate: "\ud800\u0041" is U+FFFD + 'A'.
func TestPreorderSurrogatePairFallbackKeepsSecondEscape(t *testing.T) {
	var sb strings.Builder
	err := Preorder(`"\ud800\u0041"`, &collectVisitor{sb: &sb}, nil)
	if err != nil {
		t.Fatalf("Preorder error = %v", err)
	}
	if got := sb.String(); got != "string:\ufffdA;" {
		t.Fatalf("Preorder output = %q; want \\ufffd + A", got)
	}
}

// Preorder tolerates out-of-range floats like Sonic (OnFloat64(+Inf)).
func TestPreorderOutOfRangeFloatTolerated(t *testing.T) {
	var sb strings.Builder
	err := Preorder(`1e999`, &collectVisitor{sb: &sb}, nil)
	if err != nil {
		t.Fatalf("Preorder(1e999) error = %v", err)
	}
	if !strings.Contains(sb.String(), "+Inf") {
		t.Fatalf("Preorder(1e999) output = %q; want +Inf", sb.String())
	}
}

// Preorder validates the closing delimiter of a skipped root container,
// matching Sonic. Nested mismatches remain accepted for compatibility.
func TestPreorderSkipRootDelimiterCompatibility(t *testing.T) {
	for _, input := range []string{`[1}`, `{"a":1]`} {
		if json.Valid([]byte(input)) {
			t.Fatalf("encoding/json.Valid(%q) = true, want false", input)
		}
		err := Preorder(input, &skipContainerVisitor{}, nil)
		var syntaxErr *SyntaxError
		if !errors.As(err, &syntaxErr) {
			t.Fatalf("Preorder(%q) error = %v, want *SyntaxError", input, err)
		}
	}

	const nestedMismatch = `[{"a":1]]`
	if json.Valid([]byte(nestedMismatch)) {
		t.Fatalf("encoding/json.Valid(%q) = true, want false", nestedMismatch)
	}
	if err := Preorder(nestedMismatch, &skipContainerVisitor{}, nil); err != nil {
		t.Fatalf("Preorder(%q) error = %v, want upstream-compatible acceptance", nestedMismatch, err)
	}

	const validControl = `[{"a":1}]`
	if err := Preorder(validControl, &skipContainerVisitor{}, nil); err != nil {
		t.Fatalf("Preorder(%q) error = %v", validControl, err)
	}
}

// Set returns true only when an existing key was overwritten (Sonic
// semantics); new keys report false.
func TestSetReturnSemanticsMatchSonic(t *testing.T) {
	n := NewObject(nil)
	added, err := n.Set("k", NewNumber("1"))
	if err != nil || added {
		t.Fatalf("Set(new key) = %v, %v; want false, nil", added, err)
	}
	overwrote, err := n.Set("k", NewNumber("2"))
	if err != nil || !overwrote {
		t.Fatalf("Set(existing key) = %v, %v; want true, nil", overwrote, err)
	}
}

// SetByIndex out-of-range returns ErrNotExist (Sonic) instead of
// silently growing the container.
func TestSetByIndexOutOfRangeReturnsErrNotExist(t *testing.T) {
	n := NewArray([]Node{NewNumber("1")})
	ok, err := n.SetByIndex(3, NewNumber("9"))
	if err != ErrNotExist || ok {
		t.Fatalf("SetByIndex(3) = %v, %v; want false, ErrNotExist", ok, err)
	}
	if got, _ := n.Len(); got != 1 {
		t.Fatalf("Len() = %d; want 1 (must not grow)", got)
	}
}

// Error nodes propagate their underlying error through LoadAll, Raw,
// and MarshalJSON instead of silently serializing as "null".
func TestErrorNodePropagatesThroughSerialization(t *testing.T) {
	n := NewRaw("{bad")
	if err := n.LoadAll(); err == nil {
		t.Fatalf("LoadAll() on bad JSON = nil; want error")
	}
	n2 := NewRaw("{bad")
	if _, err := n2.Raw(); err == nil {
		t.Fatalf("Raw() on bad JSON = nil; want error")
	}
	n3 := NewRaw("{bad")
	if _, err := n3.MarshalJSON(); err == nil {
		t.Fatalf("MarshalJSON() on bad JSON = nil; want error")
	}
}

// Nested V_ANY values must propagate their encoding/json failures instead of
// silently becoming null while their containing node is serialized.
func TestNestedAnyMarshalErrorPropagatesThroughSerialization(t *testing.T) {
	for _, tt := range []struct {
		name string
		node Node
	}{
		{
			name: "array",
			node: NewArray([]Node{NewAny(make(chan int))}),
		},
		{
			name: "object",
			node: NewObject([]Pair{NewPair("value", NewAny(make(chan int)))}),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.node.Raw(); err == nil {
				t.Fatal("Raw() error = nil, want unsupported chan type error")
			} else {
				var unsupported *json.UnsupportedTypeError
				if !errors.As(err, &unsupported) || unsupported.Type.Kind() != reflect.Chan {
					t.Fatalf("Raw() error = %T %v, want json.UnsupportedTypeError for chan", err, err)
				}
			}
			if _, err := tt.node.MarshalJSON(); err == nil {
				t.Fatal("MarshalJSON() error = nil, want unsupported chan type error")
			} else {
				var unsupported *json.UnsupportedTypeError
				if !errors.As(err, &unsupported) || unsupported.Type.Kind() != reflect.Chan {
					t.Fatalf("MarshalJSON() error = %T %v, want json.UnsupportedTypeError for chan", err, err)
				}
			}
		})
	}
}

// Non-strict conversions follow Sonic's wider type dispatch.
func TestNonStrictConversionsMatchSonic(t *testing.T) {
	num1 := NewRaw("1")
	if v, err := num1.Bool(); err != nil || !v {
		t.Fatalf("Bool(1) = %v, %v; want true, nil", v, err)
	}
	null := NewNull()
	if v, err := null.Bool(); err != nil || v {
		t.Fatalf("Bool(null) = %v, %v; want false, nil", v, err)
	}
	f15 := NewRaw("1.5")
	if v, err := f15.Int64(); err != nil || v != 1 {
		t.Fatalf("Int64(1.5) = %v, %v; want 1, nil", v, err)
	}
	tr := NewRaw("true")
	if v, err := tr.Int64(); err != nil || v != 1 {
		t.Fatalf("Int64(true) = %v, %v; want 1, nil", v, err)
	}
	s15 := NewString("1.5")
	if v, err := s15.Int64(); err != nil || v != 1 {
		t.Fatalf("Int64(\"1.5\") = %v, %v; want 1, nil", v, err)
	}
	if v, err := tr.Number(); err != nil || v != json.Number("1") {
		t.Fatalf("Number(true) = %v, %v; want 1, nil", v, err)
	}
	if v, err := null.Float64(); err != nil || v != 0 {
		t.Fatalf("Float64(null) = %v, %v; want 0, nil", v, err)
	}
}

// Add/Set/SetByIndex(0, ...) on V_NONE/V_NULL auto-promote the node
// (Sonic semantics) instead of erroring.
func TestMutationAutoPromotesNoneAndNull(t *testing.T) {
	var a Node
	if err := a.Add(NewBool(true)); err != nil {
		t.Fatalf("Add on V_NONE: %v", err)
	}
	if got, _ := a.Raw(); got != "[true]" {
		t.Fatalf("promoted node = %s; want [true]", got)
	}

	var o Node
	if _, err := o.Set("a", NewNumber("1")); err != nil {
		t.Fatalf("Set on V_NONE: %v", err)
	}
	if got, _ := o.Raw(); got != `{"a":1}` {
		t.Fatalf("promoted node = %s; want {\"a\":1}", got)
	}

	var z Node
	if _, err := z.SetByIndex(0, NewBool(false)); err != nil {
		t.Fatalf("SetByIndex(0) on V_NONE: %v", err)
	}
	if got, _ := z.Raw(); got != "[false]" {
		t.Fatalf("promoted node = %s; want [false]", got)
	}
}

// Index addresses object pairs; Get no longer resolves numeric keys on
// arrays (Sonic dispatch).
func TestContainerDispatchMatchesSonic(t *testing.T) {
	obj := NewObject([]Pair{NewPair("x", NewNumber("7"))})
	if v, err := obj.Index(0).Int64(); err != nil || v != 7 {
		t.Fatalf("Index(0) on object = %v, %v; want 7, nil", v, err)
	}
	arr := NewArray([]Node{NewNumber("7")})
	if got := arr.Get("0"); got.Exists() {
		t.Fatalf("Get(\"0\") on array exists; Sonic does not resolve numeric keys")
	}
}

// NewAny reports V_ANY before the first load.
func TestNewAnyReportsVAny(t *testing.T) {
	n := NewAny(42)
	if n.Type() != V_ANY {
		t.Fatalf("NewAny(42).Type() = %d; want %d (V_ANY)", n.Type(), V_ANY)
	}
}

func TestIteratorObservesNodeMutations(t *testing.T) {
	t.Run("array", func(t *testing.T) {
		array := NewArray([]Node{NewNumber("1")})
		iterator, err := array.Values()
		if err != nil {
			t.Fatalf("Values() error = %v", err)
		}
		if overwritten, err := array.SetByIndex(0, NewNumber("2")); err != nil || !overwritten {
			t.Fatalf("SetByIndex(0) = %v, %v; want true, nil", overwritten, err)
		}
		if err := array.Add(NewNumber("3")); err != nil {
			t.Fatalf("Add(3) error = %v", err)
		}
		if got := iterator.Len(); got != 2 {
			t.Fatalf("iterator.Len() = %d; want 2 after mutation", got)
		}

		for _, want := range []int64{2, 3} {
			var child Node
			if !iterator.Next(&child) {
				t.Fatalf("Next() = false; want child %d", want)
			}
			if got, err := child.Int64(); err != nil || got != want {
				t.Fatalf("Next() child = %d, %v; want %d, nil", got, err, want)
			}
		}
		if iterator.HasNext() {
			t.Fatal("HasNext() = true after consuming mutated array")
		}
	})

	t.Run("object", func(t *testing.T) {
		object := NewObject([]Pair{NewPair("one", NewNumber("1"))})
		iterator, err := object.Properties()
		if err != nil {
			t.Fatalf("Properties() error = %v", err)
		}
		if overwritten, err := object.Set("two", NewNumber("2")); err != nil || overwritten {
			t.Fatalf("Set(two) = %v, %v; want false, nil", overwritten, err)
		}
		if got := iterator.Len(); got != 2 {
			t.Fatalf("iterator.Len() = %d; want 2 after mutation", got)
		}
		if !iterator.HasNext() {
			t.Fatal("HasNext() = false after adding an object property")
		}

		var first, second Pair
		if !iterator.Next(&first) || first.Key != "one" {
			t.Fatalf("first Next() = %#v; want pair with key one", first)
		}
		if !iterator.Next(&second) || second.Key != "two" {
			t.Fatalf("second Next() = %#v; want newly added pair with key two", second)
		}
		if got, err := second.Value.Int64(); err != nil || got != 2 {
			t.Fatalf("new pair value = %d, %v; want 2, nil", got, err)
		}
	})
}

func TestSequenceString(t *testing.T) {
	if got, want := (Sequence{Index: 2}).String(), `Sequence(2, "")`; got != want {
		t.Fatalf("Sequence{Index: 2}.String() = %q; want %q", got, want)
	}
	key := "name"
	if got, want := (Sequence{Index: 3, Key: &key}).String(), `Sequence(3, "name")`; got != want {
		t.Fatalf("Sequence{Index: 3, Key: &key}.String() = %q; want %q", got, want)
	}
}

// Len returns 0 for null and absent nodes instead of erroring.
func TestLenNullReturnsZero(t *testing.T) {
	null := NewNull()
	if v, err := null.Len(); err != nil || v != 0 {
		t.Fatalf("Len(null) = %v, %v; want 0, nil", v, err)
	}
}

// Deeply nested documents (>300 levels) parse: Sonic's MAX_RECURSE is
// 4096, not fastjson's 300.
func TestDeepNestingUpTo4096Accepted(t *testing.T) {
	deep := strings.Repeat("[", 400) + "0" + strings.Repeat("]", 400)
	n, code := NewParser(deep).Parse()
	if code != 0 {
		t.Fatalf("Parse(400-deep) code = %v; want 0", code)
	}
	cur := &n
	for i := 0; i < 399; i++ {
		cur = cur.Index(0)
	}
	if v, err := cur.Index(0).Int64(); err != nil || v != 0 {
		t.Fatalf("deep value = %v, %v; want 0, nil", v, err)
	}

	s := NewSearcher(`{"a":` + deep + `}`)
	child, err := s.GetByPath("a")
	if err != nil {
		t.Fatalf("Searcher.GetByPath(400-deep) error = %v", err)
	}
	got, err := child.Raw()
	if err != nil {
		t.Fatalf("deep searcher Raw() error = %v", err)
	}
	if got != deep {
		t.Fatalf("deep searcher raw length = %d; want %d", len(got), len(deep))
	}
}

// ForEach on a scalar invokes the callback once with Index -1 (Sonic).
func TestForEachScalarInvokesOnceWithIndexMinusOne(t *testing.T) {
	n := NewNumber("5")
	var seen []int
	err := n.ForEach(func(path Sequence, node *Node) bool {
		seen = append(seen, path.Index)
		return true
	})
	if err != nil {
		t.Fatalf("ForEach(scalar) error = %v", err)
	}
	if len(seen) != 1 || seen[0] != -1 {
		t.Fatalf("ForEach(scalar) indices = %v; want [-1]", seen)
	}
}

// Pop removes the last pair of an object (Sonic).
func TestPopRemovesLastObjectPair(t *testing.T) {
	n := NewObject([]Pair{NewPair("a", NewNumber("1")), NewPair("b", NewNumber("2"))})
	if err := n.Pop(); err != nil {
		t.Fatalf("Pop(object) error = %v", err)
	}
	if got, _ := n.Len(); got != 1 {
		t.Fatalf("Len after Pop = %d; want 1", got)
	}
}

// NewSearcher defaults ValidateJSON to true (Sonic v1.15.2).
func TestNewSearcherValidateJSONDefaultsTrue(t *testing.T) {
	s := NewSearcher(`{"a":1}`)
	if !s.ValidateJSON {
		t.Fatalf("ValidateJSON default = false; want true")
	}
}

// GetByPathCopy persists CopyReturn=true on the searcher (Sonic).
func TestGetByPathCopyPersistsCopyReturn(t *testing.T) {
	s := NewSearcher(`{"a":1}`)
	if _, err := s.GetByPathCopy("a"); err != nil {
		t.Fatalf("GetByPathCopy error = %v", err)
	}
	if !s.CopyReturn {
		t.Fatalf("CopyReturn = false after GetByPathCopy; want true")
	}
}

// U+2028/U+2029 are escaped in string output like Sonic (JavaScript
// line-terminator safety).
func TestStringNodeEscapesLineSeparators(t *testing.T) {
	n := NewString("a b c")
	raw, err := n.Raw()
	if err != nil {
		t.Fatalf("Raw() error = %v", err)
	}
	want := `"a\u2028b\u2029c"`
	if raw != want {
		t.Fatalf("Raw() = %s; want %s", raw, want)
	}
}

// Loads reports the value-end position (Sonic Parser.Pos()), not
// len(src).
func TestLoadsReportsValueEndPosition(t *testing.T) {
	pos, _, err := Loads(`{"a":1}`)
	if err != nil {
		t.Fatalf("Loads error = %v", err)
	}
	if pos != 7 {
		t.Fatalf("Loads pos = %d; want 7", pos)
	}
}

func TestErrorNodeForEachPropagatesWithoutCallback(t *testing.T) {
	n := NewRaw("{")
	called := false
	if err := n.ForEach(func(Sequence, *Node) bool {
		called = true
		return true
	}); err == nil {
		t.Fatal("ForEach(V_ERROR) error = nil")
	}
	if called {
		t.Fatal("ForEach(V_ERROR) invoked callback")
	}
}

func TestGetIndexUnsupportedAndMissingSemantics(t *testing.T) {
	scalar := NewString("sonic")
	for _, tt := range []struct {
		name string
		node *Node
	}{
		{name: "Get", node: scalar.Get("k")},
		{name: "Index", node: scalar.Index(0)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.node == nil || tt.node.Type() != V_ERROR {
				t.Fatalf("scalar.%s result = %#v, want V_ERROR node", tt.name, tt.node)
			}
			if err := tt.node.Check(); !errors.Is(err, ErrUnsupportType) {
				t.Fatalf("scalar.%s Check() = %v, want ErrUnsupportType", tt.name, err)
			}
		})
	}

	object := NewObject([]Pair{NewPair("present", NewNumber("1"))})
	if got := object.Get("missing"); got != nil {
		t.Fatalf("missing object Get = %#v, want nil", got)
	}

	array := NewArray([]Node{NewNumber("1")})
	if got := array.Index(1); got != nil {
		t.Fatalf("positive out-of-range array Index = %#v, want nil", got)
	}
	if got := object.Index(1); got == nil || got.Type() != V_ERROR {
		t.Fatalf("positive out-of-range object Index = %#v, want V_ERROR", got)
	} else if err := got.Check(); !errors.Is(err, ErrNotExist) {
		t.Fatalf("positive out-of-range object Index Check() = %v, want ErrNotExist", err)
	} else if got.Error() != "value not exists" {
		t.Fatalf("positive out-of-range object Index Error() = %q, want value not exists", got.Error())
	}
	if got := ErrNotExist.Error(); got != "value not exists" {
		t.Fatalf("ErrNotExist.Error() = %q, want value not exists", got)
	}
}

func TestCapMatchesSonicContract(t *testing.T) {
	var none Node
	if got, err := none.Cap(); err != nil || got != 0 {
		t.Fatalf("V_NONE Cap() = %d, %v; want 0, nil", got, err)
	}
	null := NewNull()
	if got, err := null.Cap(); err != nil || got != 0 {
		t.Fatalf("V_NULL Cap() = %d, %v; want 0, nil", got, err)
	}

	array := Node{typ: V_ARRAY, exists: true, loaded: true, arr: make([]Node, 1, 4)}
	if got, err := array.Cap(); err != nil || got != 4 {
		t.Fatalf("array Cap() = %d, %v; want 4, nil", got, err)
	}
	object := Node{typ: V_OBJECT, exists: true, loaded: true, obj: make([]Pair, 1, 3)}
	if got, err := object.Cap(); err != nil || got != 3 {
		t.Fatalf("object Cap() = %d, %v; want 3, nil", got, err)
	}

	for _, tt := range []struct {
		name string
		node Node
	}{
		{name: "string", node: NewString("sonic")},
		{name: "number", node: NewNumber("1")},
		{name: "bool", node: NewBool(true)},
		{name: "V_ANY", node: NewAny(1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := tt.node.Cap(); got != 0 || !errors.Is(err, ErrUnsupportType) {
				t.Fatalf("%s Cap() = %d, %v; want 0, ErrUnsupportType", tt.name, got, err)
			}
		})
	}

	var nilNode *Node
	if got, err := nilNode.Cap(); got != 0 || !errors.Is(err, ErrNotExist) {
		t.Fatalf("nil Cap() = %d, %v; want 0, ErrNotExist", got, err)
	}
}

func TestNewAnyPreservesOriginalValues(t *testing.T) {
	nilAny := NewAny(nil)
	if nilAny.Type() != V_ANY {
		t.Fatalf("NewAny(nil).Type() = %d, want V_ANY", nilAny.Type())
	}
	if got, err := nilAny.Interface(); err != nil || got != nil {
		t.Fatalf("NewAny(nil).Interface() = %#v, %v; want nil, nil", got, err)
	}

	mapValue := map[string]int{"initial": 1}
	mapAny := NewAny(mapValue)
	mapValue["from-source"] = 2
	got, err := mapAny.Interface()
	if err != nil {
		t.Fatalf("NewAny(map).Interface() error = %v", err)
	}
	retained, ok := got.(map[string]int)
	if !ok || retained["from-source"] != 2 {
		t.Fatalf("NewAny(map).Interface() = %#v, want original map with source mutation", got)
	}
	retained["from-interface"] = 3
	if mapValue["from-interface"] != 3 {
		t.Fatalf("NewAny(map) did not preserve map identity: %#v", mapValue)
	}
	if _, err := mapAny.String(); !errors.Is(err, ErrUnsupportType) {
		t.Fatalf("NewAny(map).String() error = %v, want ErrUnsupportType", err)
	}

	child := NewNumber("7")
	pointerAny := NewAny(&child)
	if pointerAny.Type() != V_NUMBER {
		t.Fatalf("NewAny(*Node).Type() = %d, want V_NUMBER", pointerAny.Type())
	}
	if got, err := pointerAny.Interface(); err != nil || got != float64(7) {
		t.Fatalf("NewAny(*Node).Interface() = %#v, %v; want float64(7), nil", got, err)
	}

	intAny := NewAny(42)
	if got, err := intAny.Interface(); err != nil {
		t.Fatalf("NewAny(42).Interface() error = %v", err)
	} else if value, ok := got.(int); !ok || value != 42 {
		t.Fatalf("NewAny(42).Interface() = %#v (%T), want int(42)", got, got)
	}
	if got, err := intAny.InterfaceUseNumber(); err != nil {
		t.Fatalf("NewAny(42).InterfaceUseNumber() error = %v", err)
	} else if value, ok := got.(int); !ok || value != 42 {
		t.Fatalf("NewAny(42).InterfaceUseNumber() = %#v (%T), want int(42)", got, got)
	}

	t.Run("nil Node pointer panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("NewAny(nil *Node) did not panic")
			}
		}()
		var node *Node
		_ = NewAny(node)
	})
}

func TestNewAnyPreservesNumericConversions(t *testing.T) {
	integer := NewAny(42)
	if got, err := integer.Int64(); err != nil || got != 42 {
		t.Fatalf("NewAny(42).Int64() = %d, %v; want 42, nil", got, err)
	}
	if got, err := integer.Float64(); err != nil || got != 42 {
		t.Fatalf("NewAny(42).Float64() = %v, %v; want 42, nil", got, err)
	}
	if got, err := integer.Number(); err != nil || got != json.Number("1") {
		t.Fatalf("NewAny(42).Number() = %q, %v; want 1, nil", got, err)
	}
	if got, err := integer.String(); err != nil || got != "42" {
		t.Fatalf("NewAny(42).String() = %q, %v; want 42, nil", got, err)
	}
	if got, err := integer.StrictInt64(); err != nil || got != 42 {
		t.Fatalf("NewAny(42).StrictInt64() = %d, %v; want 42, nil", got, err)
	}
	if _, err := integer.StrictFloat64(); !errors.Is(err, ErrUnsupportType) {
		t.Fatalf("NewAny(42).StrictFloat64() error = %v, want ErrUnsupportType", err)
	}
	if _, err := integer.StrictNumber(); !errors.Is(err, ErrUnsupportType) {
		t.Fatalf("NewAny(42).StrictNumber() error = %v, want ErrUnsupportType", err)
	}

	maxUint64 := uint64(math.MaxUint64)
	maxUint := NewAny(maxUint64)
	if got, err := maxUint.String(); err != nil || got != strconv.Itoa(int(maxUint64)) {
		t.Fatalf("NewAny(MaxUint64).String() = %q, %v; want strconv.Itoa(int(MaxUint64)) = %q, nil", got, err, strconv.Itoa(int(maxUint64)))
	}
	if got, err := maxUint.Number(); err != nil || got != json.Number("1") {
		t.Fatalf("NewAny(MaxUint64).Number() = %q, %v; want 1, nil", got, err)
	}
	if got, err := maxUint.StrictInt64(); err != nil || got != int64(maxUint64) {
		t.Fatalf("NewAny(MaxUint64).StrictInt64() = %d, %v; want int64(MaxUint64) = %d, nil", got, err, int64(maxUint64))
	}

	float32Input := float32(1.5)
	float32Value := NewAny(float32Input)
	if got, err := float32Value.String(); err != nil || got != strconv.FormatFloat(float64(float32Input), 'g', -1, 64) {
		t.Fatalf("NewAny(float32(1.5)).String() = %q, %v; want FormatFloat(float64(v), 'g', -1, 64) = %q, nil", got, err, strconv.FormatFloat(float64(float32Input), 'g', -1, 64))
	}
	if got, err := float32Value.Number(); err != nil || got != json.Number("1") {
		t.Fatalf("NewAny(float32(1.5)).Number() = %q, %v; want 1, nil", got, err)
	}
	if got, err := float32Value.Float64(); err != nil || got != 1.5 {
		t.Fatalf("NewAny(float32(1.5)).Float64() = %v, %v; want 1.5, nil", got, err)
	}
	if got, err := float32Value.StrictFloat64(); err != nil || got != 1.5 {
		t.Fatalf("NewAny(float32(1.5)).StrictFloat64() = %v, %v; want 1.5, nil", got, err)
	}
	if _, err := float32Value.StrictNumber(); !errors.Is(err, ErrUnsupportType) {
		t.Fatalf("NewAny(float32(1.5)).StrictNumber() error = %v, want ErrUnsupportType", err)
	}

	jsonNumber := NewAny(json.Number("12.5"))
	if got, err := jsonNumber.Number(); err != nil || got != json.Number("12.5") {
		t.Fatalf("NewAny(json.Number).Number() = %q, %v; want 12.5, nil", got, err)
	}
	if got, err := jsonNumber.StrictNumber(); err != nil || got != json.Number("12.5") {
		t.Fatalf("NewAny(json.Number).StrictNumber() = %q, %v; want 12.5, nil", got, err)
	}
	if got, err := jsonNumber.String(); err != nil || got != "12.5" {
		t.Fatalf("NewAny(json.Number).String() = %q, %v; want 12.5, nil", got, err)
	}
	if _, err := jsonNumber.StrictFloat64(); !errors.Is(err, ErrUnsupportType) {
		t.Fatalf("NewAny(json.Number).StrictFloat64() error = %v, want ErrUnsupportType", err)
	}

	for _, tt := range []struct {
		value      bool
		wantNumber json.Number
		wantString string
	}{
		{value: true, wantNumber: json.Number("1"), wantString: "true"},
		{value: false, wantNumber: json.Number("0"), wantString: "false"},
	} {
		node := NewAny(tt.value)
		if got, err := node.Number(); err != nil || got != tt.wantNumber {
			t.Fatalf("NewAny(%t).Number() = %q, %v; want %q, nil", tt.value, got, err, tt.wantNumber)
		}
		if got, err := node.String(); err != nil || got != tt.wantString {
			t.Fatalf("NewAny(%t).String() = %q, %v; want %q, nil", tt.value, got, err, tt.wantString)
		}
	}

	numericString := NewAny("42")
	if got, err := numericString.Int64(); err != nil || got != 42 {
		t.Fatalf("NewAny(\"42\").Int64() = %d, %v; want 42, nil", got, err)
	}
	if got, err := numericString.Float64(); err != nil || got != 42 {
		t.Fatalf("NewAny(\"42\").Float64() = %v, %v; want 42, nil", got, err)
	}
	if got, err := numericString.Number(); err != nil || got != json.Number("42") {
		t.Fatalf("NewAny(\"42\").Number() = %q, %v; want 42, nil", got, err)
	}
	if got, err := numericString.StrictString(); err != nil || got != "42" {
		t.Fatalf("NewAny(\"42\").StrictString() = %q, %v; want 42, nil", got, err)
	}
	if _, err := integer.StrictString(); !errors.Is(err, ErrUnsupportType) {
		t.Fatalf("NewAny(42).StrictString() error = %v, want ErrUnsupportType", err)
	}
}

func TestNewAnyContainerUseNodeReturnsOriginalValues(t *testing.T) {
	array := []Node{NewNumber("1")}
	arrayAny := NewAny(array)
	gotArray, err := arrayAny.ArrayUseNode()
	if err != nil || len(gotArray) != 1 {
		t.Fatalf("NewAny([]Node).ArrayUseNode() = %#v, %v; want original one-element []Node, nil", gotArray, err)
	}
	gotArray[0] = NewNumber("2")
	if got, err := array[0].Raw(); err != nil || got != "2" {
		t.Fatalf("ArrayUseNode() did not return original slice: raw = %q, %v; want 2, nil", got, err)
	}

	object := map[string]Node{"x": NewNumber("1")}
	objectAny := NewAny(object)
	gotObject, err := objectAny.MapUseNode()
	if err != nil || len(gotObject) != 1 {
		t.Fatalf("NewAny(map[string]Node).MapUseNode() = %#v, %v; want original one-entry map, nil", gotObject, err)
	}
	gotObject["x"] = NewNumber("2")
	objectNode := object["x"]
	if got, err := objectNode.Raw(); err != nil || got != "2" {
		t.Fatalf("MapUseNode() did not return original map: raw = %q, %v; want 2, nil", got, err)
	}

	integer := NewAny(42)
	if _, err := integer.ArrayUseNode(); !errors.Is(err, ErrUnsupportType) {
		t.Fatalf("NewAny(42).ArrayUseNode() error = %v; want ErrUnsupportType", err)
	}
	if _, err := integer.MapUseNode(); !errors.Is(err, ErrUnsupportType) {
		t.Fatalf("NewAny(42).MapUseNode() error = %v; want ErrUnsupportType", err)
	}
}

func TestNewBytesEmptyPanics(t *testing.T) {
	for _, src := range [][]byte{nil, {}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("NewBytes(%#v) did not panic", src)
				}
			}()
			_ = NewBytes(src)
		}()
	}
}

// Parser advances incrementally: scalar tokens consume themselves, while
// leading whitespace is only part of the recognition step.
func TestParserParseScalarIncrementalCursor(t *testing.T) {
	p := NewParser(" 1 true")

	first, code := p.Parse()
	if code != 0 {
		t.Fatalf("first Parse code = %v", code)
	}
	if raw, err := first.Raw(); err != nil || raw != "1" {
		t.Fatalf("first Parse Raw() = %q, %v; want 1, nil", raw, err)
	}
	if p.Pos() != 2 {
		t.Fatalf("first Parse Pos = %d; want 2", p.Pos())
	}

	second, code := p.Parse()
	if code != 0 {
		t.Fatalf("second Parse code = %v", code)
	}
	if raw, err := second.Raw(); err != nil || raw != "true" {
		t.Fatalf("second Parse Raw() = %q, %v; want true, nil", raw, err)
	}
	if p.Pos() != 7 {
		t.Fatalf("second Parse Pos = %d; want 7", p.Pos())
	}

	if _, code := p.Parse(); code != nativetypes.ERR_EOF {
		t.Fatalf("third Parse code = %v; want ERR_EOF", code)
	}
	if p.Pos() != 7 {
		t.Fatalf("third Parse Pos = %d; want 7", p.Pos())
	}
}

// Empty containers consume their closing delimiter, including intervening
// JSON whitespace.
func TestParserParseEmptyContainersConsumeCloser(t *testing.T) {
	p := NewParser(" [ ] {}")

	array, code := p.Parse()
	if code != 0 || array.Type() != V_ARRAY {
		t.Fatalf("array Parse = type %d, code %v; want V_ARRAY, 0", array.Type(), code)
	}
	if p.Pos() != 4 {
		t.Fatalf("array Parse Pos = %d; want 4", p.Pos())
	}

	object, code := p.Parse()
	if code != 0 || object.Type() != V_OBJECT {
		t.Fatalf("object Parse = type %d, code %v; want V_OBJECT, 0", object.Type(), code)
	}
	if p.Pos() != 7 {
		t.Fatalf("object Parse Pos = %d; want 7", p.Pos())
	}
}

// Non-empty containers are independently lazy, but leave Parser at the first
// byte after the opener so successive calls can parse their interior.
func TestParserParseNonEmptyContainersAreLazyAndIncremental(t *testing.T) {
	t.Run("array", func(t *testing.T) {
		p := NewParser(" [ 1,2]")
		node, code := p.Parse()
		if code != 0 || node.Type() != V_ARRAY {
			t.Fatalf("Parse = type %d, code %v; want V_ARRAY, 0", node.Type(), code)
		}
		if p.Pos() != 2 {
			t.Fatalf("container Parse Pos = %d; want 2", p.Pos())
		}
		if got, err := node.Index(0).Int64(); err != nil || got != 1 {
			t.Fatalf("node.Index(0).Int64() = %d, %v; want 1, nil", got, err)
		}
		if p.Pos() != 2 {
			t.Fatalf("loading returned node changed Pos to %d; want 2", p.Pos())
		}
		child, code := p.Parse()
		if code != 0 {
			t.Fatalf("interior Parse code = %v", code)
		}
		if raw, err := child.Raw(); err != nil || raw != "1" {
			t.Fatalf("interior Parse Raw() = %q, %v; want 1, nil", raw, err)
		}
		if p.Pos() != 4 {
			t.Fatalf("interior Parse Pos = %d; want 4", p.Pos())
		}
	})

	t.Run("object", func(t *testing.T) {
		p := NewParser(` { "a":1}`)
		node, code := p.Parse()
		if code != 0 || node.Type() != V_OBJECT {
			t.Fatalf("Parse = type %d, code %v; want V_OBJECT, 0", node.Type(), code)
		}
		if p.Pos() != 2 {
			t.Fatalf("container Parse Pos = %d; want 2", p.Pos())
		}
		if got, err := node.Get("a").Int64(); err != nil || got != 1 {
			t.Fatalf("node.Get(\"a\").Int64() = %d, %v; want 1, nil", got, err)
		}
		if p.Pos() != 2 {
			t.Fatalf("loading returned node changed Pos to %d; want 2", p.Pos())
		}
		key, code := p.Parse()
		if code != 0 {
			t.Fatalf("interior Parse code = %v", code)
		}
		if raw, err := key.Raw(); err != nil || raw != `"a"` {
			t.Fatalf("interior Parse Raw() = %q, %v; want \"a\", nil", raw, err)
		}
		if p.Pos() != 6 {
			t.Fatalf("interior Parse Pos = %d; want 6", p.Pos())
		}
	})
}

// Invalid scalar and string attempts preserve their original starting cursor,
// even if recognition skipped leading whitespace.
func TestParserParseMalformedAttemptsKeepStartPosition(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  string
		want nativetypes.ParsingError
	}{
		{name: "invalid character after whitespace", src: "  @", want: nativetypes.ERR_INVALID_CHAR},
		{name: "unterminated string", src: "\"unterminated", want: nativetypes.ERR_EOF},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(tt.src)
			if _, code := p.Parse(); code != tt.want {
				t.Fatalf("Parse(%q) code = %v; want %v", tt.src, code, tt.want)
			}
			if p.Pos() != 0 {
				t.Fatalf("Parse(%q) Pos = %d; want 0", tt.src, p.Pos())
			}
			if got := p.ExportError(tt.want).Error(); !strings.Contains(got, "at index 0") {
				t.Fatalf("ExportError(%v) = %q; want absolute index 0", tt.want, got)
			}
		})
	}
}

// A non-empty malformed container is returned as a deferred raw node. Its
// opening delimiter advances Parser, while loading is responsible for error.
func TestParserParseMalformedContainersDeferErrors(t *testing.T) {
	for _, tt := range []struct {
		src string
		typ int
	}{
		{src: "[", typ: V_ARRAY},
		{src: "{", typ: V_OBJECT},
		{src: "[1,}", typ: V_ARRAY},
	} {
		t.Run(tt.src, func(t *testing.T) {
			p := NewParser(tt.src)
			node, code := p.Parse()
			if code != 0 || node.Type() != tt.typ {
				t.Fatalf("Parse(%q) = type %d, code %v; want type %d, 0", tt.src, node.Type(), code, tt.typ)
			}
			if p.Pos() != 1 {
				t.Fatalf("Parse(%q) Pos = %d; want 1", tt.src, p.Pos())
			}
			if err := node.LoadAll(); err == nil {
				t.Fatalf("Parse(%q).LoadAll() error = nil; want deferred error", tt.src)
			}
		})
	}
}

// A comma is not itself a value: after an array opener and its first scalar,
// Parse reports the original comma offset.
func TestParserParseNonEmptyArrayCommaKeepsAttemptOffset(t *testing.T) {
	p := NewParser("[1,2]")
	if _, code := p.Parse(); code != 0 || p.Pos() != 1 {
		t.Fatalf("container Parse = code %v, Pos %d; want 0, 1", code, p.Pos())
	}
	if _, code := p.Parse(); code != 0 || p.Pos() != 2 {
		t.Fatalf("scalar Parse = code %v, Pos %d; want 0, 2", code, p.Pos())
	}
	if _, code := p.Parse(); code != nativetypes.ERR_INVALID_CHAR {
		t.Fatalf("comma Parse code = %v; want ERR_INVALID_CHAR", code)
	}
	if p.Pos() != 2 {
		t.Fatalf("comma Parse Pos = %d; want 2", p.Pos())
	}
	if got := p.ExportError(nativetypes.ERR_INVALID_CHAR).Error(); !strings.Contains(got, "at index 2") {
		t.Fatalf("comma ExportError = %q; want absolute index 2", got)
	}
}

// collectVisitor records scalar callbacks for Preorder tests.
type collectVisitor struct {
	sb *strings.Builder
}

type skipContainerVisitor struct{}

func (*skipContainerVisitor) OnNull() error                    { return nil }
func (*skipContainerVisitor) OnBool(bool) error                { return nil }
func (*skipContainerVisitor) OnString(string) error            { return nil }
func (*skipContainerVisitor) OnInt64(int64, json.Number) error { return nil }
func (*skipContainerVisitor) OnFloat64(float64, json.Number) error {
	return nil
}
func (*skipContainerVisitor) OnObjectBegin(int) error  { return VisitOPSkip }
func (*skipContainerVisitor) OnObjectKey(string) error { return nil }
func (*skipContainerVisitor) OnObjectEnd() error       { return nil }
func (*skipContainerVisitor) OnArrayBegin(int) error   { return VisitOPSkip }
func (*skipContainerVisitor) OnArrayEnd() error        { return nil }

func (v *collectVisitor) OnNull() error           { v.sb.WriteString("null;"); return nil }
func (v *collectVisitor) OnBool(b bool) error     { v.sb.WriteString("bool;"); return nil }
func (v *collectVisitor) OnString(s string) error { v.sb.WriteString("string:" + s + ";"); return nil }
func (v *collectVisitor) OnInt64(i int64, n json.Number) error {
	v.sb.WriteString("int:")
	v.sb.WriteString(n.String())
	v.sb.WriteString(";")
	return nil
}
func (v *collectVisitor) OnFloat64(f float64, n json.Number) error {
	v.sb.WriteString("float:")
	v.sb.WriteString(n.String())
	if f > 1e308 {
		v.sb.WriteString("(+Inf)")
	}
	v.sb.WriteString(";")
	return nil
}
func (v *collectVisitor) OnObjectBegin(capacity int) error { return nil }
func (v *collectVisitor) OnObjectKey(key string) error     { return nil }
func (v *collectVisitor) OnObjectEnd() error               { return nil }
func (v *collectVisitor) OnArrayBegin(capacity int) error  { return nil }
func (v *collectVisitor) OnArrayEnd() error                { return nil }
