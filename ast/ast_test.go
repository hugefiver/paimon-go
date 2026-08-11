package ast

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestNodeParseLookupMutationAndSort(t *testing.T) {
	n := NewRaw(`{"b":2,"a":[true,{"x":"y"}]}`)
	if err := n.LoadAll(); err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	if got, err := n.Get("a").Index(1).Get("x").String(); err != nil || got != "y" {
		t.Fatalf("path string = %q, %v; want y, nil", got, err)
	}
	changed, err := n.Set("c", NewNumber("3"))
	if err != nil || !changed {
		t.Fatalf("Set(c) = %v, %v; want true, nil", changed, err)
	}
	if err := n.SortKeys(false); err != nil {
		t.Fatalf("SortKeys(false) error = %v", err)
	}
	raw, err := n.Raw()
	if err != nil {
		t.Fatalf("Raw() error = %v", err)
	}
	if raw != `{"a":[true,{"x":"y"}],"b":2,"c":3}` {
		t.Fatalf("Raw() = %s", raw)
	}
}

func TestRawPreservesRawControlByteInStringLikeSonic(t *testing.T) {
	data := string([]byte{'{', '"', 0x11, 'x', '"', ':', '1', '}'})
	n := NewRaw(data)
	raw, err := n.Raw()
	if err != nil {
		t.Fatalf("Raw() error = %v", err)
	}
	if raw != data {
		t.Fatalf("Raw() = %q, want original raw-control JSON %q", raw, data)
	}
}

func TestPublicTypeConstantsMatchSonic(t *testing.T) {
	cases := map[string]int{
		"V_NONE":   V_NONE,
		"V_ERROR":  V_ERROR,
		"V_NULL":   V_NULL,
		"V_TRUE":   V_TRUE,
		"V_FALSE":  V_FALSE,
		"V_ARRAY":  V_ARRAY,
		"V_OBJECT": V_OBJECT,
		"V_STRING": V_STRING,
		"V_NUMBER": V_NUMBER,
		"V_ANY":    V_ANY,
	}
	want := map[string]int{
		"V_NONE":   0,
		"V_ERROR":  1,
		"V_NULL":   2,
		"V_TRUE":   3,
		"V_FALSE":  4,
		"V_ARRAY":  5,
		"V_OBJECT": 6,
		"V_STRING": 7,
		"V_NUMBER": 33,
		"V_ANY":    34,
	}
	for name, got := range cases {
		if got != want[name] {
			t.Fatalf("%s = %d, want %d", name, got, want[name])
		}
	}
}

func TestNewRawInvalidReturnsErrorNode(t *testing.T) {
	n := NewRaw(`{"bad":`)
	if got := n.Type(); got != V_ERROR {
		t.Fatalf("Type() = %d, want V_ERROR", got)
	}
	if n.Valid() {
		t.Fatalf("invalid raw node Valid() = true")
	}
	if n.Error() == "" {
		t.Fatalf("invalid raw Error() is empty")
	}
}

func TestNewRawValidUnloadedNodeIsValid(t *testing.T) {
	n := NewRaw(`{"ok":true}`)
	if got := n.Type(); got != V_ANY {
		t.Fatalf("Type() = %d, want V_ANY", got)
	}
	if !n.IsRaw() {
		t.Fatalf("IsRaw() = false, want true")
	}
	if !n.Valid() {
		t.Fatalf("unloaded valid raw node Valid() = false")
	}
}

func TestSyntaxErrorMethods(t *testing.T) {
	se := SyntaxError{Pos: 1, Src: `{"bad":`, Msg: "custom"}
	if se.Message() != "custom" {
		t.Fatalf("Message() = %q, want custom", se.Message())
	}
	if se.Description() == "" {
		t.Fatalf("Description() is empty")
	}
	if se.Error() == "" {
		t.Fatalf("Error() is empty")
	}
}

func TestNodeConversionsAndMissingValues(t *testing.T) {
	n := NewObject([]Pair{NewPair("n", NewNumber("42")), NewPair("s", NewString("ok"))})
	if got, err := n.Get("n").Int64(); err != nil || got != 42 {
		t.Fatalf("Int64() = %d, %v; want 42, nil", got, err)
	}
	if got, err := n.Get("n").Number(); err != nil || got != json.Number("42") {
		t.Fatalf("Number() = %q, %v; want 42, nil", got, err)
	}
	missing := n.Get("missing")
	if missing.Exists() || !missing.Valid() {
		t.Fatalf("missing Exists/Valid = %v/%v, want false/true", missing.Exists(), missing.Valid())
	}
	if _, err := missing.String(); !errors.Is(err, ErrNotExist) {
		t.Fatalf("missing.String() error = %v, want ErrNotExist", err)
	}
}

func TestNodeExistsValidCheckMatchSonicStatePredicates(t *testing.T) {
	root := NewRaw(`{"x":1}`)
	missing := root.Get("missing")
	if missing.Exists() {
		t.Fatalf("missing Exists() = true, want false")
	}
	if !missing.Valid() {
		t.Fatalf("missing Valid() = false, want true")
	}
	if err := missing.Check(); err != nil {
		t.Fatalf("missing Check() = %v, want nil", err)
	}

	var zero Node
	if zero.Exists() {
		t.Fatalf("zero Exists() = true, want false")
	}
	if !zero.Valid() {
		t.Fatalf("zero Valid() = false, want true")
	}
	if err := zero.Check(); err != nil {
		t.Fatalf("zero Check() = %v, want nil", err)
	}

	errNode := NewRaw(`{"bad":`)
	if errNode.Exists() {
		t.Fatalf("error node Exists() = true, want false")
	}
	if errNode.Valid() {
		t.Fatalf("error node Valid() = true, want false")
	}
	if err := errNode.Check(); err == nil {
		t.Fatalf("error node Check() = nil, want error")
	}

	validRaw := NewRaw(`{"x":1}`)
	if !validRaw.Exists() {
		t.Fatalf("valid raw Exists() = false, want true")
	}
	if !validRaw.Valid() {
		t.Fatalf("valid raw Valid() = false, want true")
	}
	if err := validRaw.Check(); err != nil {
		t.Fatalf("valid raw Check() = %v, want nil", err)
	}

	var nilNode *Node
	if nilNode.Exists() {
		t.Fatalf("nil Exists() = true, want false")
	}
	if nilNode.Valid() {
		t.Fatalf("nil Valid() = true, want false")
	}
	if err := nilNode.Check(); !errors.Is(err, ErrNotExist) {
		t.Fatalf("nil Check() = %v, want ErrNotExist", err)
	}
}

func TestIteratorsAndForEach(t *testing.T) {
	n := NewArray([]Node{NewString("a"), NewString("b")})
	vals, err := n.Values()
	if err != nil {
		t.Fatalf("Values() error = %v", err)
	}
	var got []string
	for vals.HasNext() {
		var child Node
		if !vals.Next(&child) {
			t.Fatalf("Next returned false while HasNext was true")
		}
		s, err := child.String()
		if err != nil {
			t.Fatalf("child.String() error = %v", err)
		}
		got = append(got, s)
	}
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("iterated values = %#v", got)
	}
}

type recordingVisitor struct{ events []string }

func (v *recordingVisitor) OnNull() error { v.events = append(v.events, "null"); return nil }
func (v *recordingVisitor) OnBool(x bool) error {
	if x {
		v.events = append(v.events, "true")
	} else {
		v.events = append(v.events, "false")
	}
	return nil
}
func (v *recordingVisitor) OnString(s string) error {
	v.events = append(v.events, "string:"+s)
	return nil
}
func (v *recordingVisitor) OnInt64(_ int64, n json.Number) error {
	v.events = append(v.events, "int:"+n.String())
	return nil
}
func (v *recordingVisitor) OnFloat64(_ float64, n json.Number) error {
	v.events = append(v.events, "float:"+n.String())
	return nil
}
func (v *recordingVisitor) OnObjectBegin(capacity int) error {
	v.events = append(v.events, "object")
	return nil
}
func (v *recordingVisitor) OnObjectKey(key string) error {
	v.events = append(v.events, "key:"+key)
	return nil
}
func (v *recordingVisitor) OnObjectEnd() error { v.events = append(v.events, "object-end"); return nil }
func (v *recordingVisitor) OnArrayBegin(capacity int) error {
	v.events = append(v.events, "array")
	return nil
}
func (v *recordingVisitor) OnArrayEnd() error { v.events = append(v.events, "array-end"); return nil }

func TestPreorderVisitor(t *testing.T) {
	visitor := &recordingVisitor{}
	if err := Preorder(`{"a":[1,"x",null]}`, visitor, nil); err != nil {
		t.Fatalf("Preorder() error = %v", err)
	}
	want := []string{"object", "key:a", "array", "int:1", "string:x", "null", "array-end", "object-end"}
	if !reflect.DeepEqual(visitor.events, want) {
		t.Fatalf("events = %#v, want %#v", visitor.events, want)
	}
}

func TestSearcherAndParser(t *testing.T) {
	s := NewSearcher(`{"users":[{"id":1},{"id":2}]}`)
	n, err := s.GetByPath("users", 1, "id")
	if err != nil {
		t.Fatalf("GetByPath() error = %v", err)
	}
	if got, err := n.Int64(); err != nil || got != 2 {
		t.Fatalf("id = %d, %v; want 2, nil", got, err)
	}
	p := NewParser(`{"ok":true)`)
	_, perr := p.Parse()
	if perr == 0 {
		t.Fatalf("Parse invalid JSON returned no ParsingError")
	}
}

func TestNewBytesBase64(t *testing.T) {
	src := []byte("hello")
	n := NewBytes(src)
	// NewBytes must produce a base64-encoded string node, not raw JSON.
	if got := n.Type(); got != V_STRING {
		t.Fatalf("NewBytes node type = %d, want V_STRING(%d)", got, V_STRING)
	}
	s, err := n.String()
	if err != nil {
		t.Fatalf("String() error = %v", err)
	}
	if want := base64.StdEncoding.EncodeToString(src); s != want {
		t.Fatalf("NewBytes string = %q, want base64 %q", s, want)
	}
	// Round-trip decode must produce the original bytes.
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64 decode error = %v", err)
	}
	if string(decoded) != "hello" {
		t.Fatalf("decoded = %q, want %q", decoded, "hello")
	}
}
