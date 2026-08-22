package ast

import (
	"encoding/json"
	"strings"
	"testing"
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

// collectVisitor records scalar callbacks for Preorder tests.
type collectVisitor struct {
	sb *strings.Builder
}

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
