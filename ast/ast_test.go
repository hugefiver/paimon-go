package ast

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
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
	if err != nil || changed {
		t.Fatalf("Set(c) = %v, %v; want false, nil (Sonic: new key reports false)", changed, err)
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
	if got := n.Type(); got != V_OBJECT {
		t.Fatalf("Type() = %d, want V_OBJECT", got)
	}
	if !n.IsRaw() {
		t.Fatalf("IsRaw() = false, want true")
	}
	if !n.Valid() {
		t.Fatalf("unloaded valid raw node Valid() = false")
	}
}

func TestNewRawReportsConcreteTypeBeforeLoad(t *testing.T) {
	for _, tt := range []struct {
		name    string
		input   string
		wantTyp int
		wantRaw string
		object  bool
	}{
		{name: "null", input: `null`, wantTyp: V_NULL, wantRaw: `null`},
		{name: "true", input: `true`, wantTyp: V_TRUE, wantRaw: `true`},
		{name: "false", input: `false`, wantTyp: V_FALSE, wantRaw: `false`},
		{name: "array", input: `[]`, wantTyp: V_ARRAY, wantRaw: `[]`},
		{name: "object", input: `{}`, wantTyp: V_OBJECT, wantRaw: `{}`},
		{name: "string", input: `"x"`, wantTyp: V_STRING, wantRaw: `"x"`},
		{name: "number", input: `-1.5e2`, wantTyp: V_NUMBER, wantRaw: `-1.5e2`},
		{name: "first value only", input: `  {"x":1} trailing`, wantTyp: V_OBJECT, wantRaw: `{"x":1}`, object: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			n := NewRaw(tt.input)
			if got := n.Type(); got != tt.wantTyp {
				t.Fatalf("Type() = %d, want %d", got, tt.wantTyp)
			}
			if !n.IsRaw() {
				t.Fatal("IsRaw() = false, want true before Load")
			}
			if got, err := n.Raw(); err != nil || got != tt.wantRaw {
				t.Fatalf("Raw() = %q, %v; want %q, nil", got, err, tt.wantRaw)
			}
			if tt.object {
				if err := n.LoadAll(); err != nil {
					t.Fatalf("LoadAll() error = %v", err)
				}
				if got, err := n.Get("x").Int64(); err != nil || got != 1 {
					t.Fatalf("Get(\"x\").Int64() = %d, %v; want 1, nil", got, err)
				}
			}
		})
	}
}

func TestMarshalJSONPreservesUnloadedRawChildrenFirstValue(t *testing.T) {
	arrayWithScalar := NewArray([]Node{NewRaw(`1 trailing`)})
	objectWithArray := NewObject([]Pair{NewPair("array", NewRaw(` [true, false] trailing`))})
	arrayWithObject := NewArray(nil)
	if err := arrayWithObject.Add(NewRaw(`{"nested": 1} trailing`)); err != nil {
		t.Fatalf("Add(raw object) error = %v", err)
	}
	objectWithString := NewObject(nil)
	if added, err := objectWithString.Set("string", NewRaw(`"value" trailing`)); err != nil || added {
		t.Fatalf("Set(raw string) = %v, %v; want false, nil (Sonic: new key reports false)", added, err)
	}

	for _, tt := range []struct {
		name string
		node Node
		want string
	}{
		{name: "constructor embeds raw scalar", node: arrayWithScalar, want: `[1]`},
		{name: "constructor embeds raw array", node: objectWithArray, want: `{"array":[true, false]}`},
		{name: "Add embeds raw object", node: arrayWithObject, want: `[{"nested": 1}]`},
		{name: "Set embeds raw string", node: objectWithString, want: `{"string":"value"}`},
		{name: "top level retains first raw value", node: NewRaw(` {"top": 1} trailing`), want: `{"top": 1}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.node.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("MarshalJSON() = %s, want raw first value %s", got, tt.want)
			}
			encoded, err := json.Marshal(&tt.node)
			if err != nil {
				t.Fatalf("json.Marshal(&Node) error = %v", err)
			}
			if compactJSON(string(encoded)) != compactJSON(tt.want) {
				t.Fatalf("json.Marshal(&Node) = %s, want raw first value equivalent to %s", encoded, tt.want)
			}
		})
	}
}

func compactJSON(s string) string {
	var dst bytes.Buffer
	if err := json.Compact(&dst, []byte(s)); err != nil {
		return s
	}
	return dst.String()
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

type recordedFloat struct {
	value float64
	raw   json.Number
}

type recordingVisitor struct {
	events     []string
	intCalls   []json.Number
	floatCalls []recordedFloat
}

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
	v.intCalls = append(v.intCalls, n)
	return nil
}
func (v *recordingVisitor) OnFloat64(f float64, n json.Number) error {
	v.events = append(v.events, "float:"+n.String())
	v.floatCalls = append(v.floatCalls, recordedFloat{value: f, raw: n})
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

func TestPreorderOnlyNumberMatchesSonic(t *testing.T) {
	visitor := &recordingVisitor{}
	if err := Preorder(`{"i":1,"f":1.5}`, visitor, &VisitorOptions{OnlyNumber: true}); err != nil {
		t.Fatalf("Preorder() error = %v", err)
	}
	if len(visitor.intCalls) != 0 {
		t.Fatalf("OnInt64 calls = %#v, want none", visitor.intCalls)
	}
	if got, want := visitor.floatCalls, []recordedFloat{
		{value: 0, raw: json.Number("1")},
		{value: 0, raw: json.Number("1.5")},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("OnFloat64 calls = %#v, want %#v", got, want)
	}

	if err := Preorder(`1e`, &recordingVisitor{}, &VisitorOptions{OnlyNumber: true}); err == nil {
		t.Fatal("Preorder(1e) error = nil, want SyntaxError")
	} else if _, ok := err.(*SyntaxError); !ok {
		t.Fatalf("Preorder(1e) error = %T, want *SyntaxError", err)
	}
}

func TestPreorderInvalidUTF8MatchesSonic(t *testing.T) {
	invalidLead := string([]byte{0xe4})
	invalidContinuation := string([]byte{0xc3, 0x28})
	invalidThreeByte := string([]byte{0xe2, 0x28, 0xa1})

	for _, tt := range []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "object key invalid lead",
			input: `{"d` + invalidLead + `":1}`,
			want:  []string{"object", "key:d" + invalidLead, "float:1", "object-end"},
		},
		{
			name:  "string value invalid lead",
			input: `{"k":"d` + invalidLead + `"}`,
			want:  []string{"object", "key:k", "string:d" + invalidLead, "object-end"},
		},
		{
			name:  "root string invalid lead",
			input: `"d` + invalidLead + `"`,
			want:  []string{"string:d" + invalidLead},
		},
		{
			name:  "object key invalid continuation",
			input: `{"d` + invalidContinuation + `":1}`,
			want:  []string{"object", "key:d" + invalidContinuation, "float:1", "object-end"},
		},
		{
			name:  "string value invalid continuation",
			input: `{"k":"d` + invalidContinuation + `"}`,
			want:  []string{"object", "key:k", "string:d" + invalidContinuation, "object-end"},
		},
		{
			name:  "object key invalid three byte sequence",
			input: `{"d` + invalidThreeByte + `":1}`,
			want:  []string{"object", "key:d" + invalidThreeByte, "float:1", "object-end"},
		},
		{
			name:  "string value invalid three byte sequence",
			input: `{"k":"d` + invalidThreeByte + `"}`,
			want:  []string{"object", "key:k", "string:d" + invalidThreeByte, "object-end"},
		},
		{
			name:  "valid utf8",
			input: `"dé"`,
			want:  []string{"string:dé"},
		},
		{
			name:  "fuzz corpus retains every number",
			input: `{"users":[{"id":1},{"d` + invalidLead + `":2}]}`,
			want: []string{
				"object", "key:users", "array", "object", "key:id", "float:1", "object-end",
				"object", "key:d" + invalidLead, "float:2", "object-end", "array-end", "object-end",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			visitor := &recordingVisitor{}
			if err := Preorder(tt.input, visitor, &VisitorOptions{OnlyNumber: true}); err != nil {
				t.Fatalf("Preorder() error = %T: %v", err, err)
			}
			if !reflect.DeepEqual(visitor.events, tt.want) {
				t.Fatalf("events = %#v, want %#v", visitor.events, tt.want)
			}
			for _, call := range visitor.floatCalls {
				if call.value != 0 {
					t.Fatalf("OnFloat64 value = %v, want 0 when OnlyNumber is true", call.value)
				}
			}
		})
	}

	visitor := &recordingVisitor{}
	if err := Preorder("\"a\x01\"", visitor, &VisitorOptions{OnlyNumber: true}); err != nil {
		t.Fatalf("Preorder(raw control) error = %v", err)
	}
	if want := []string{"string:a\x01"}; !reflect.DeepEqual(visitor.events, want) {
		t.Fatalf("raw control events = %#v, want %#v", visitor.events, want)
	}
}

type capacityVisitor struct {
	objectCapacity int
	arrayCapacity  int
}

func (*capacityVisitor) OnNull() error                    { return nil }
func (*capacityVisitor) OnBool(bool) error                { return nil }
func (*capacityVisitor) OnString(string) error            { return nil }
func (*capacityVisitor) OnInt64(int64, json.Number) error { return nil }
func (*capacityVisitor) OnFloat64(float64, json.Number) error {
	return nil
}
func (v *capacityVisitor) OnObjectBegin(capacity int) error {
	v.objectCapacity = capacity
	return nil
}
func (*capacityVisitor) OnObjectKey(string) error { return nil }
func (*capacityVisitor) OnObjectEnd() error       { return nil }
func (v *capacityVisitor) OnArrayBegin(capacity int) error {
	v.arrayCapacity = capacity
	return nil
}
func (*capacityVisitor) OnArrayEnd() error { return nil }

func TestPreorderKeepsEstimatedContainerCapacity(t *testing.T) {
	visitor := &capacityVisitor{}
	if err := Preorder(`{}`, visitor, nil); err != nil {
		t.Fatalf("Preorder({}) error = %v", err)
	}
	if visitor.objectCapacity != 0 {
		t.Fatalf("object capacity = %d, want 0", visitor.objectCapacity)
	}

	visitor = &capacityVisitor{}
	if err := Preorder(`[1]`, visitor, nil); err != nil {
		t.Fatalf("Preorder([1]) error = %v", err)
	}
	if visitor.arrayCapacity != 1 {
		t.Fatalf("array capacity = %d, want 1", visitor.arrayCapacity)
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

func TestInt64PathOverflowIsMissing(t *testing.T) {
	if strconv.IntSize != 32 {
		t.Skip("int64-to-int path overflow requires a 32-bit architecture")
	}

	n := NewRaw(`["zero"]`)
	if n.GetByPath(int64(1 << 32)).Exists() {
		t.Fatal("Node.GetByPath(int64(1 << 32)).Exists() = true, want false")
	}

	_, err := NewSearcher(`["zero"]`).GetByPath(int64(1 << 32))
	if !errors.Is(err, ErrNotExist) {
		t.Fatalf("Searcher.GetByPath(int64(1 << 32)) error = %v, want ErrNotExist", err)
	}
}

func TestSearcherEscapedObjectKey(t *testing.T) {
	for _, tt := range []struct {
		json string
		key  string
	}{
		{json: `{"\u0061":1}`, key: "a"},
		{json: `{"a\/b":1}`, key: "a/b"},
		{json: `{"a\"b":1}`, key: `a"b`},
	} {
		n, err := NewSearcher(tt.json).GetByPath(tt.key)
		if err != nil {
			t.Fatalf("GetByPath(%s, %q) error = %v", tt.json, tt.key, err)
		}
		if got, err := n.Int64(); err != nil || got != 1 {
			t.Fatalf("GetByPath(%s, %q) = %d, %v; want 1, nil", tt.json, tt.key, got, err)
		}
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
