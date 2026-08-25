package upstream_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/version"
	"math"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/decoder"
	"github.com/bytedance/sonic/encoder"
)

type upstreamCountingMarshaler struct {
	calls int
}

func (m *upstreamCountingMarshaler) MarshalJSON() ([]byte, error) {
	m.calls++
	return []byte(`{"value":"<tag>"}`), nil
}

func TestUpstreamMarshalIndentCallsMarshalerOnce(t *testing.T) {
	value := &upstreamCountingMarshaler{}
	got, err := sonic.Config{}.Froze().MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent error = %v", err)
	}
	if value.calls != 1 {
		t.Fatalf("MarshalJSON calls = %d, want 1; output = %s", value.calls, got)
	}
}

func TestEncoderOptionBitsStayIndependent(t *testing.T) {
	tests := []struct {
		name        string
		opts        encoder.Options
		wantEscaped bool
	}{
		{name: "SortMapKeys", opts: encoder.SortMapKeys, wantEscaped: false},
		{name: "EscapeHTML", opts: encoder.EscapeHTML, wantEscaped: true},
		{name: "CompactMarshaler", opts: encoder.CompactMarshaler, wantEscaped: false},
		{name: "CompatibleWithStd", opts: encoder.CompatibleWithStd, wantEscaped: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []byte
			if err := encoder.EncodeInto(&got, map[string]string{"x": "<tag>"}, tt.opts); err != nil {
				t.Fatalf("EncodeInto error = %v", err)
			}
			escaped := bytes.Contains(got, []byte(`\u003c`))
			if escaped != tt.wantEscaped {
				t.Fatalf("EncodeInto escaped < = %v; want %v (output %s)", escaped, tt.wantEscaped, got)
			}
		})
	}

	var stdlib bytes.Buffer
	stdlibEncoder := json.NewEncoder(&stdlib)
	stdlibEncoder.SetEscapeHTML(false)
	if err := stdlibEncoder.Encode(map[string]string{"x": "<tag>"}); err != nil {
		t.Fatalf("stdlib Encode error = %v", err)
	}
	if bytes.Contains(stdlib.Bytes(), []byte(`\u003c`)) {
		t.Fatalf("stdlib output escaped <: %s", stdlib.Bytes())
	}
}

func TestUpstreamUseNumberAndUseInt64Resolution(t *testing.T) {
	if version.Compare(runtime.Version(), "go1.27") < 0 {
		t.Run("native rejects combined options", func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("Froze UseNumber and UseInt64 configuration did not panic when decoding")
				}
			}()
			api := sonic.Config{UseNumber: true, UseInt64: true}.Froze()
			var scalar interface{}
			_ = api.Unmarshal([]byte(`1`), &scalar)
		})
		return
	}

	t.Run("fallback UseNumber precedence", func(t *testing.T) {
		api := sonic.Config{UseNumber: true, UseInt64: true}.Froze()

		var scalar interface{}
		if err := api.Unmarshal([]byte(`1`), &scalar); err != nil {
			t.Fatalf("Unmarshal scalar error = %v", err)
		}
		if got, ok := scalar.(json.Number); !ok || got.String() != "1" {
			t.Fatalf("scalar = %v (%T), want json.Number(1)", scalar, scalar)
		}
		t.Logf("scalar = %v (%T)", scalar, scalar)

		var nested map[string]interface{}
		if err := api.Unmarshal([]byte(`{"n":1,"a":[2]}`), &nested); err != nil {
			t.Fatalf("Unmarshal nested error = %v", err)
		}
		array, ok := nested["a"].([]interface{})
		if !ok || len(array) != 1 {
			t.Fatalf("a = %v (%T), want one-element []interface{}", nested["a"], nested["a"])
		}
		if got, ok := nested["n"].(json.Number); !ok || got.String() != "1" {
			t.Fatalf("nested.n = %v (%T), want json.Number(1)", nested["n"], nested["n"])
		}
		if got, ok := array[0].(json.Number); !ok || got.String() != "2" {
			t.Fatalf("nested.a[0] = %v (%T), want json.Number(2)", array[0], array[0])
		}
		t.Logf("nested.n = %v (%T); nested.a[0] = %v (%T)", nested["n"], nested["n"], array[0], array[0])

		var stdlib map[string]interface{}
		stdlibDecoder := json.NewDecoder(bytes.NewReader([]byte(`{"n":1,"a":[2]}`)))
		stdlibDecoder.UseNumber()
		if err := stdlibDecoder.Decode(&stdlib); err != nil {
			t.Fatalf("stdlib Decode error = %v", err)
		}
		stdlibArray := stdlib["a"].([]interface{})
		if got := stdlib["n"].(json.Number); got.String() != "1" {
			t.Fatalf("stdlib n = %q, want 1", got)
		}
		if got := stdlibArray[0].(json.Number); got.String() != "2" {
			t.Fatalf("stdlib a[0] = %q, want 2", got)
		}
	})
}

func TestUpstreamDecoderOptionsConflictPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("decoder.SetOptions did not panic")
		}
	}()
	decoder.NewDecoder(`1`).SetOptions(decoder.OptionUseNumber | decoder.OptionUseInt64)
}

type upstreamSkipVisitor struct{}

func (*upstreamSkipVisitor) OnNull() error                        { return nil }
func (*upstreamSkipVisitor) OnBool(bool) error                    { return nil }
func (*upstreamSkipVisitor) OnString(string) error                { return nil }
func (*upstreamSkipVisitor) OnInt64(int64, json.Number) error     { return nil }
func (*upstreamSkipVisitor) OnFloat64(float64, json.Number) error { return nil }
func (*upstreamSkipVisitor) OnObjectBegin(int) error              { return ast.VisitOPSkip }
func (*upstreamSkipVisitor) OnObjectKey(string) error             { return nil }
func (*upstreamSkipVisitor) OnObjectEnd() error                   { return nil }
func (*upstreamSkipVisitor) OnArrayBegin(int) error               { return ast.VisitOPSkip }
func (*upstreamSkipVisitor) OnArrayEnd() error                    { return nil }

func TestUpstreamPreorderSkipRootDelimiterCompatibility(t *testing.T) {
	for _, input := range []string{`[1}`, `{"a":1]`} {
		if err := ast.Preorder(input, &upstreamSkipVisitor{}, nil); err == nil {
			t.Fatalf("Preorder(%q) error = nil, want syntax error", input)
		}
	}
	const nestedMismatch = `[{"a":1]]`
	if json.Valid([]byte(nestedMismatch)) {
		t.Fatalf("encoding/json.Valid(%q) = true, want false", nestedMismatch)
	}
	if err := ast.Preorder(nestedMismatch, &upstreamSkipVisitor{}, nil); err != nil {
		t.Fatalf("Preorder(%q) error = %v, want upstream-compatible acceptance", nestedMismatch, err)
	}
}

func TestUpstreamFullReviewNoCopyRawMessageNilMarshal(t *testing.T) {
	var message sonic.NoCopyRawMessage
	got, err := message.MarshalJSON()
	if err != nil {
		t.Fatalf("nil NoCopyRawMessage.MarshalJSON() error = %v", err)
	}
	if string(got) != "null" {
		t.Fatalf("nil NoCopyRawMessage.MarshalJSON() = %q, want null", got)
	}
}

func TestUpstreamFullReviewEncoderPrefixIndentAndNilBuffer(t *testing.T) {
	native := version.Compare(runtime.Version(), "go1.27") < 0
	var enc encoder.Encoder
	enc.SetIndent("P", "")
	got, err := enc.Encode(map[string]int{"a": 1})
	if err != nil {
		t.Fatalf("Encoder.Encode error = %v", err)
	}
	want := "{\nP\"a\": 1\nP}\n"
	if native {
		want = "{\nP\"a\": 1\nP}"
	}
	if string(got) != want {
		t.Fatalf("prefix-only SetIndent output = %q, want %q", got, want)
	}
	if native {
		var buffer []byte
		if err := encoder.EncodeInto(&buffer, map[string]int{"a": 1}, 0); err != nil {
			t.Fatalf("EncodeInto(non-nil) error = %v", err)
		}
		if len(buffer) == 0 {
			t.Fatal("EncodeInto(non-nil) returned an empty buffer")
		}
		return
	}

	defer func() {
		if recover() == nil {
			t.Fatal("EncodeInto(nil, ...) did not panic")
		}
	}()
	_ = encoder.EncodeInto(nil, map[string]int{"a": 1}, 0)
}

func TestUpstreamFullReviewDecoderErrorLiteralsCompile(t *testing.T) {
	if version.Compare(runtime.Version(), "go1.27") < 0 {
		for _, tt := range []struct {
			name string
			typ  reflect.Type
			want []string
		}{
			{name: "SyntaxError", typ: reflect.TypeOf(decoder.SyntaxError{}), want: []string{"Pos", "Src", "Code", "Msg"}},
			{name: "MismatchTypeError", typ: reflect.TypeOf(decoder.MismatchTypeError{}), want: []string{"Pos", "Src", "Type"}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, field := range tt.want {
					if _, ok := tt.typ.FieldByName(field); !ok {
						t.Fatalf("%s has no exported %s field", tt.name, field)
					}
				}
			})
		}
		return
	}

	for _, tt := range []struct {
		name string
		typ  reflect.Type
		want []string
	}{
		{name: "SyntaxError", typ: reflect.TypeOf(decoder.SyntaxError{}), want: []string{"Offset"}},
		{name: "MismatchTypeError", typ: reflect.TypeOf(decoder.MismatchTypeError{}), want: []string{"Value", "Type", "Offset", "Struct", "Field"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, field := range tt.want {
				if _, ok := tt.typ.FieldByName(field); !ok {
					t.Fatalf("%s has no exported %s field", tt.name, field)
				}
			}
		})
	}
}

func TestUpstreamFullReviewParserScalarsWhitespaceAndContainers(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  string
		want interface{}
	}{
		{name: "null", src: "null", want: nil},
		{name: "true", src: "true", want: true},
		{name: "false", src: "false", want: false},
		{name: "string", src: `"sonic"`, want: "sonic"},
		{name: "number", src: "12", want: float64(12)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parser := ast.NewParser(tt.src)
			node, code := parser.Parse()
			if code != 0 {
				t.Fatalf("Parse(%q) code = %v", tt.src, code)
			}
			if parser.Pos() != len(tt.src) {
				t.Fatalf("Parse(%q) Pos = %d, want %d", tt.src, parser.Pos(), len(tt.src))
			}
			got, err := node.Interface()
			if err != nil {
				t.Fatalf("Parse(%q).Interface error = %v", tt.src, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Parse(%q).Interface = %#v, want %#v", tt.src, got, tt.want)
			}
		})
	}

	whitespace := ast.NewParser(" \t true \n")
	whitespaceNode, code := whitespace.Parse()
	if code != 0 {
		t.Fatalf("Parse with whitespace code = %v", code)
	}
	if whitespace.Pos() != 7 {
		t.Fatalf("Parse with whitespace Pos = %d, want 7", whitespace.Pos())
	}
	if got, err := whitespaceNode.Interface(); err != nil || got != true {
		t.Fatalf("Parse with whitespace Interface = %#v, %v; want true, nil", got, err)
	}

	for _, tt := range []struct {
		name     string
		src      string
		wantType int
		want     interface{}
	}{
		{name: "empty array", src: "[]", wantType: ast.V_ARRAY, want: []interface{}{}},
		{name: "empty object", src: "{}", wantType: ast.V_OBJECT, want: map[string]interface{}{}},
		{name: "nonempty array", src: "[1,2]", wantType: ast.V_ARRAY, want: []interface{}{float64(1), float64(2)}},
		{name: "nonempty object", src: `{"a":1}`, wantType: ast.V_OBJECT, want: map[string]interface{}{"a": float64(1)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parser := ast.NewParser(tt.src)
			node, code := parser.Parse()
			if code != 0 {
				t.Fatalf("Parse(%q) code = %v", tt.src, code)
			}
			wantPos := len(tt.src)
			if tt.src[0] == '[' || tt.src[0] == '{' {
				if len(tt.src) > 2 {
					wantPos = 1
				}
			}
			if parser.Pos() != wantPos {
				t.Fatalf("Parse(%q) Pos = %d, want %d", tt.src, parser.Pos(), wantPos)
			}
			if node.Type() != tt.wantType {
				t.Fatalf("Parse(%q).Type = %d, want %d", tt.src, node.Type(), tt.wantType)
			}
			got, err := node.Interface()
			if err != nil {
				t.Fatalf("Parse(%q).Interface error = %v", tt.src, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Parse(%q).Interface = %#v, want %#v", tt.src, got, tt.want)
			}
		})
	}

	interior := ast.NewParser("[1,2]")
	if _, code := interior.Parse(); code != 0 {
		t.Fatalf("initial Parse from container code = %v", code)
	}
	if interior.Pos() != 1 {
		t.Fatalf("initial Parse from container Pos = %d, want 1", interior.Pos())
	}
	first, code := interior.Parse()
	if code != 0 {
		t.Fatalf("successive Parse from container interior code = %v", code)
	}
	if interior.Pos() != 2 {
		t.Fatalf("successive Parse from container interior Pos = %d, want 2", interior.Pos())
	}
	if got, err := first.Interface(); err != nil || got != float64(1) {
		t.Fatalf("successive Parse from container interior Interface = %#v, %v; want 1, nil", got, err)
	}

	for _, src := range []string{"!", " \t!"} {
		parser := ast.NewParser(src)
		if _, code := parser.Parse(); code == 0 {
			t.Fatalf("Parse(%q) code = 0, want malformed-token error", src)
		}
		wantPos := 0
		if version.Compare(runtime.Version(), "go1.27") < 0 {
			wantPos = len(src) - 1
		}
		if parser.Pos() != wantPos {
			t.Fatalf("Parse(%q) malformed-token Pos = %d, want %d", src, parser.Pos(), wantPos)
		}
	}
}

func TestUpstreamFullReviewParserMalformedContainersDeferErrors(t *testing.T) {
	for _, src := range []string{"[1}", `{"a":1]`} {
		parser := ast.NewParser(src)
		node, code := parser.Parse()
		if code != 0 {
			t.Fatalf("Parse(%q) code = %v, want deferred error", src, code)
		}
		if parser.Pos() != 1 {
			t.Fatalf("Parse(%q) Pos = %d, want 1 after opener", src, parser.Pos())
		}
		if _, err := node.Interface(); err == nil {
			t.Fatalf("Parse(%q).Interface error = nil, want deferred malformed-container error", src)
		}
	}
}

func TestUpstreamFullReviewMalformedRawForEachDoesNotCallback(t *testing.T) {
	node := ast.NewRaw("[1}")
	if node.Type() != ast.V_ERROR {
		t.Fatalf("NewRaw malformed node type = %d, want V_ERROR", node.Type())
	}
	called := false
	if err := node.ForEach(func(ast.Sequence, *ast.Node) bool {
		called = true
		return true
	}); err == nil {
		t.Fatal("V_ERROR ForEach error = nil")
	}
	if called {
		t.Fatal("V_ERROR ForEach invoked callback")
	}
}

func TestUpstreamFullReviewNodeLookupAndIndexSemantics(t *testing.T) {
	object := ast.NewObject([]ast.Pair{ast.NewPair("present", ast.NewNumber("1"))})
	if got := object.Get("missing"); got != nil {
		t.Fatalf("missing object Get = %#v, want nil", got)
	}

	scalar := ast.NewString("sonic")
	if got := scalar.Get("missing"); got == nil || got.Type() != ast.V_ERROR {
		t.Fatalf("scalar Get type = %v, want V_ERROR", got)
	}
	if got := scalar.Index(0); got == nil || got.Type() != ast.V_ERROR {
		t.Fatalf("scalar Index type = %v, want V_ERROR", got)
	}

	array := ast.NewArray(nil)
	if got := array.Index(1); got != nil {
		t.Fatalf("positive out-of-range array Index = %#v, want nil", got)
	}
	if got := object.Index(1); got == nil || got.Error() != "value not exists" {
		t.Fatalf("positive out-of-range object Index = %#v, want error value not exists", got)
	}
}

func TestUpstreamFullReviewNewAnySemantics(t *testing.T) {
	nilAny := ast.NewAny(nil)
	if nilAny.Type() != ast.V_ANY {
		t.Fatalf("NewAny(nil).Type = %d, want V_ANY", nilAny.Type())
	}
	if got, err := nilAny.Interface(); err != nil || got != nil {
		t.Fatalf("NewAny(nil).Interface = %#v, %v; want nil, nil", got, err)
	}

	mapValue := map[string]int{"initial": 1}
	mapAny := ast.NewAny(mapValue)
	got, err := mapAny.Interface()
	if err != nil {
		t.Fatalf("NewAny(map).Interface error = %v", err)
	}
	retained, ok := got.(map[string]int)
	if !ok {
		t.Fatalf("NewAny(map).Interface type = %T, want map[string]int", got)
	}
	retained["from-interface"] = 2
	if mapValue["from-interface"] != 2 {
		t.Fatalf("NewAny(map) did not retain map identity after Interface mutation: %#v", mapValue)
	}
	mapValue["from-source"] = 3
	again, err := mapAny.Interface()
	if err != nil || again.(map[string]int)["from-source"] != 3 {
		t.Fatalf("NewAny(map) did not retain source mutation: %#v, %v", again, err)
	}

	child := ast.NewNumber("7")
	pointerAny := ast.NewAny(&child)
	if pointerAny.Type() != ast.V_NUMBER {
		t.Fatalf("NewAny(*Node).Type = %d, want V_NUMBER", pointerAny.Type())
	}
	if got, err := pointerAny.Interface(); err != nil || got != float64(7) {
		t.Fatalf("NewAny(*Node).Interface = %#v, %v; want 7, nil", got, err)
	}
}

// TestUpstreamNestedAnyMarshalErrorsPropagate witnesses the Go 1.27 fallback
// behavior of Sonic v1.15.2: nested V_ANY encoding errors are returned by both
// Raw and MarshalJSON rather than serialized as null.
func TestUpstreamNestedAnyMarshalErrorsPropagate(t *testing.T) {
	for _, tt := range []struct {
		name string
		node ast.Node
	}{
		{
			name: "array",
			node: ast.NewArray([]ast.Node{ast.NewAny(make(chan int))}),
		},
		{
			name: "object",
			node: ast.NewObject([]ast.Pair{ast.NewPair("value", ast.NewAny(make(chan int)))}),
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

// TestUpstreamFinalReviewNewAnyConversionAndContainerWitness records the
// deliberately narrow V_ANY conversion dispatch used by Sonic v1.15.2.
func TestUpstreamFinalReviewNewAnyConversionAndContainerWitness(t *testing.T) {
	integer := ast.NewAny(42)
	if got, err := integer.Interface(); err != nil || got != int(42) {
		t.Fatalf("NewAny(42).Interface() = %#v, %v; want int(42), nil", got, err)
	}
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
	if _, err := integer.StrictFloat64(); !errors.Is(err, ast.ErrUnsupportType) {
		t.Fatalf("NewAny(42).StrictFloat64() error = %v; want ErrUnsupportType", err)
	}
	if _, err := integer.StrictNumber(); !errors.Is(err, ast.ErrUnsupportType) {
		t.Fatalf("NewAny(42).StrictNumber() error = %v; want ErrUnsupportType", err)
	}

	maxUint64 := uint64(math.MaxUint64)
	maxUint := ast.NewAny(maxUint64)
	if got, err := maxUint.Number(); err != nil || got != json.Number("1") {
		t.Fatalf("NewAny(MaxUint64).Number() = %q, %v; want 1, nil", got, err)
	}
	if got, err := maxUint.String(); err != nil || got != strconv.Itoa(int(maxUint64)) {
		t.Fatalf("NewAny(MaxUint64).String() = %q, %v; want strconv.Itoa(int(MaxUint64)) = %q", got, err, strconv.Itoa(int(maxUint64)))
	}
	if got, err := maxUint.StrictInt64(); err != nil || got != int64(maxUint64) {
		t.Fatalf("NewAny(MaxUint64).StrictInt64() = %d, %v; want int64(MaxUint64) = %d, nil", got, err, int64(maxUint64))
	}

	float32Value := float32(1.5)
	float32Any := ast.NewAny(float32Value)
	if got, err := float32Any.Number(); err != nil || got != json.Number("1") {
		t.Fatalf("NewAny(float32(1.5)).Number() = %q, %v; want 1, nil", got, err)
	}
	if got, err := float32Any.String(); err != nil || got != strconv.FormatFloat(float64(float32Value), 'g', -1, 64) {
		t.Fatalf("NewAny(float32(1.5)).String() = %q, %v; want FormatFloat(float64(v), 'g', -1, 64) = %q", got, err, strconv.FormatFloat(float64(float32Value), 'g', -1, 64))
	}
	if got, err := float32Any.StrictFloat64(); err != nil || got != 1.5 {
		t.Fatalf("NewAny(float32(1.5)).StrictFloat64() = %v, %v; want 1.5, nil", got, err)
	}
	if _, err := float32Any.StrictNumber(); !errors.Is(err, ast.ErrUnsupportType) {
		t.Fatalf("NewAny(float32(1.5)).StrictNumber() error = %v; want ErrUnsupportType", err)
	}

	number := json.Number("12.5")
	numberAny := ast.NewAny(number)
	if got, err := numberAny.Number(); err != nil || got != number {
		t.Fatalf("NewAny(json.Number).Number() = %q, %v; want %q, nil", got, err, number)
	}
	if got, err := numberAny.String(); err != nil || got != number.String() {
		t.Fatalf("NewAny(json.Number).String() = %q, %v; want %q, nil", got, err, number)
	}
	if got, err := numberAny.StrictNumber(); err != nil || got != number {
		t.Fatalf("NewAny(json.Number).StrictNumber() = %q, %v; want %q, nil", got, err, number)
	}
	if _, err := numberAny.StrictFloat64(); !errors.Is(err, ast.ErrUnsupportType) {
		t.Fatalf("NewAny(json.Number).StrictFloat64() error = %v; want ErrUnsupportType", err)
	}

	array := []ast.Node{ast.NewNumber("1")}
	arrayAny := ast.NewAny(array)
	if got, err := arrayAny.ArrayUseNode(); err != nil || !reflect.DeepEqual(got, array) {
		t.Fatalf("NewAny([]Node).ArrayUseNode() = %#v, %v; want original []Node contents, nil", got, err)
	}
	object := map[string]ast.Node{"x": ast.NewNumber("1")}
	objectAny := ast.NewAny(object)
	if got, err := objectAny.MapUseNode(); err != nil || !reflect.DeepEqual(got, object) {
		t.Fatalf("NewAny(map[string]Node).MapUseNode() = %#v, %v; want original map contents, nil", got, err)
	}
	if _, err := integer.ArrayUseNode(); !errors.Is(err, ast.ErrUnsupportType) {
		t.Fatalf("NewAny(42).ArrayUseNode() error = %v; want ErrUnsupportType", err)
	}
	if _, err := integer.MapUseNode(); !errors.Is(err, ast.ErrUnsupportType) {
		t.Fatalf("NewAny(42).MapUseNode() error = %v; want ErrUnsupportType", err)
	}
}

func TestUpstreamFinalReviewEncodeIntoNilBufferPanicLiteral(t *testing.T) {
	if version.Compare(runtime.Version(), "go1.27") < 0 {
		defer func() {
			if recover() == nil {
				t.Fatal("native EncodeInto(nil, ...) did not panic")
			}
		}()
		_ = encoder.EncodeInto(nil, nil, 0)
		return
	}

	defer func() {
		if got := recover(); got != "user-supplied buffer buf is nil" {
			t.Fatalf("EncodeInto(nil, ...) panic = %#v; want exact upstream literal", got)
		}
	}()
	_ = encoder.EncodeInto(nil, map[string]int{"a": 1}, 0)
}

func TestUpstreamFullReviewNewAnyNilNodePointerPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewAny(nil *Node) did not panic")
		}
	}()
	var node *ast.Node
	_ = ast.NewAny(node)
}

func TestUpstreamFullReviewCapacityAndNewBytes(t *testing.T) {
	array := ast.NewArray([]ast.Node{ast.NewNumber("1")})
	if capacity, err := array.Cap(); err != nil || capacity < 1 {
		t.Fatalf("array Cap = %d, %v; want capacity >= 1, nil", capacity, err)
	}
	object := ast.NewObject([]ast.Pair{ast.NewPair("a", ast.NewNumber("1"))})
	if capacity, err := object.Cap(); err != nil || capacity < 1 {
		t.Fatalf("object Cap = %d, %v; want capacity >= 1, nil", capacity, err)
	}

	for _, tt := range []struct {
		name string
		node ast.Node
	}{
		{name: "V_NONE"},
		{name: "V_NULL", node: ast.NewNull()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if capacity, err := tt.node.Cap(); err != nil || capacity != 0 {
				t.Fatalf("%s Cap = %d, %v; want 0, nil", tt.name, capacity, err)
			}
		})
	}

	for _, tt := range []struct {
		name string
		node ast.Node
	}{
		{name: "scalar", node: ast.NewString("sonic")},
		{name: "V_ANY", node: ast.NewAny(1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.node.Cap(); err != ast.ErrUnsupportType {
				t.Fatalf("%s Cap error = %v, want ErrUnsupportType", tt.name, err)
			}
		})
	}

	defer func() {
		if recover() == nil {
			t.Fatal("NewBytes(empty) did not panic")
		}
	}()
	_ = ast.NewBytes(nil)
}

func TestUpstreamFullReviewIteratorsObserveMutation(t *testing.T) {
	array := ast.NewArray([]ast.Node{ast.NewNumber("1")})
	values, err := array.Values()
	if err != nil {
		t.Fatalf("array Values error = %v", err)
	}
	if err := array.Add(ast.NewNumber("2")); err != nil {
		t.Fatalf("array Add error = %v", err)
	}
	for want := int64(1); want <= 2; want++ {
		var got ast.Node
		if !values.Next(&got) {
			t.Fatalf("array iterator stopped before %d", want)
		}
		value, err := got.Int64()
		if err != nil || value != want {
			t.Fatalf("array iterator value = %d, %v; want %d, nil", value, err, want)
		}
	}
	if values.Next(&ast.Node{}) {
		t.Fatal("array iterator returned an unexpected third value")
	}

	object := ast.NewObject([]ast.Pair{ast.NewPair("a", ast.NewNumber("1"))})
	properties, err := object.Properties()
	if err != nil {
		t.Fatalf("object Properties error = %v", err)
	}
	if replaced, err := object.Set("b", ast.NewNumber("2")); err != nil || replaced {
		t.Fatalf("object Set = replaced:%v, error:%v; want false, nil", replaced, err)
	}
	for _, want := range []struct {
		key   string
		value int64
	}{
		{key: "a", value: 1},
		{key: "b", value: 2},
	} {
		var got ast.Pair
		if !properties.Next(&got) {
			t.Fatalf("object iterator stopped before %q", want.key)
		}
		value, err := got.Value.Int64()
		if got.Key != want.key || err != nil || value != want.value {
			t.Fatalf("object iterator pair = %q:%d, %v; want %q:%d, nil", got.Key, value, err, want.key, want.value)
		}
	}
	if properties.Next(&ast.Pair{}) {
		t.Fatal("object iterator returned an unexpected third pair")
	}
}

func TestUpstreamFullReviewSequenceString(t *testing.T) {
	if got := (ast.Sequence{Index: 2}).String(); got != `Sequence(2, "")` {
		t.Fatalf("Sequence{Index: 2}.String() = %q, want %q", got, `Sequence(2, "")`)
	}
	key := "name"
	if got := (ast.Sequence{Index: 3, Key: &key}).String(); got != `Sequence(3, "name")` {
		t.Fatalf("Sequence{Index: 3, Key: name}.String() = %q, want %q", got, `Sequence(3, "name")`)
	}
}

type upstreamPerformanceReviewUnmarshaler struct {
	calls int
}

func (u *upstreamPerformanceReviewUnmarshaler) UnmarshalJSON(data []byte) error {
	u.calls++
	var value interface{}
	return json.Unmarshal(data, &value)
}

type upstreamPerformanceReviewEnvelope struct {
	Value  interface{}
	Custom upstreamPerformanceReviewUnmarshaler
}

func TestUpstreamPerformanceReviewNumberModes(t *testing.T) {
	native := version.Compare(runtime.Version(), "go1.27") < 0
	tests := []struct {
		name      string
		cfg       sonic.Config
		useNumber bool
	}{
		{name: "UseNumber", cfg: sonic.Config{UseNumber: true, CaseSensitive: true, DisallowUnknownFields: true}, useNumber: true},
		{name: "UseInt64", cfg: sonic.Config{UseInt64: true, CaseSensitive: true, DisallowUnknownFields: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNumber := func(label string, got interface{}, want int64) {
				if tt.useNumber {
					if number, ok := got.(json.Number); !ok || number.String() != strconv.FormatInt(want, 10) {
						t.Fatalf("%s = %#v (%T), want json.Number(%d)", label, got, got, want)
					}
					return
				}
				if native {
					if number, ok := got.(int64); !ok || number != want {
						t.Fatalf("%s = %#v (%T), want int64(%d)", label, got, got, want)
					}
					return
				}
				if number, ok := got.(float64); !ok || number != float64(want) {
					t.Fatalf("%s = %#v (%T), want float64(%d)", label, got, got, want)
				}
			}

			api := tt.cfg.Froze()
			var out upstreamPerformanceReviewEnvelope
			if err := api.Unmarshal([]byte(`{"Value":{"integer":1,"array":[2]},"Custom":{"ok":true}}`), &out); err != nil {
				t.Fatalf("Unmarshal nested interface values error = %v", err)
			}
			if out.Custom.calls != 1 {
				t.Fatalf("custom UnmarshalJSON calls = %d, want 1", out.Custom.calls)
			}
			nested, ok := out.Value.(map[string]interface{})
			if !ok {
				t.Fatalf("Value type = %T, want map[string]interface{}", out.Value)
			}
			array, ok := nested["array"].([]interface{})
			if !ok || len(array) != 1 {
				t.Fatalf("Value.array = %#v (%T), want one-element []interface{}", nested["array"], nested["array"])
			}
			assertNumber("Value.integer", nested["integer"], 1)
			assertNumber("Value.array[0]", array[0], 2)

			var caseSensitive upstreamPerformanceReviewEnvelope
			err := api.Unmarshal([]byte(`{"value":1}`), &caseSensitive)
			if native && err == nil {
				t.Fatal("native CaseSensitive and DisallowUnknownFields accepted lower-case Value")
			}
			if !native && err != nil {
				t.Fatalf("fallback CaseSensitive and DisallowUnknownFields rejected lower-case Value: %v", err)
			}

			var duplicate interface{}
			if err := api.Unmarshal([]byte(`1`), &duplicate); err != nil {
				t.Fatalf("first scalar Unmarshal error = %v", err)
			}
			if err := api.Unmarshal([]byte(`2`), &duplicate); err != nil {
				t.Fatalf("second scalar Unmarshal error = %v", err)
			}
			assertNumber("duplicate scalar", duplicate, 2)

			var invalidUTF8 upstreamPerformanceReviewEnvelope
			if err := api.Unmarshal([]byte("{\"Value\":\"\xff\"}"), &invalidUTF8); err != nil {
				t.Fatalf("Unmarshal invalid UTF-8 error = %v", err)
			}
			wantInvalidUTF8 := "\xff"
			if !native {
				wantInvalidUTF8 = "\ufffd"
			}
			if got, ok := invalidUTF8.Value.(string); !ok || got != wantInvalidUTF8 {
				t.Fatalf("invalid UTF-8 Value = %q (%T), want %q", invalidUTF8.Value, invalidUTF8.Value, wantInvalidUTF8)
			}

			stream := api.NewDecoder(strings.NewReader(`{"Value":{"integer":1,"array":[2]}}`))
			var streamed map[string]interface{}
			if err := stream.Decode(&streamed); err != nil {
				t.Fatalf("stream Decode error = %v", err)
			}
			streamedValue, ok := streamed["Value"].(map[string]interface{})
			if !ok {
				t.Fatalf("streamed Value type = %T, want map[string]interface{}", streamed["Value"])
			}
			streamedArray, ok := streamedValue["array"].([]interface{})
			if !ok || len(streamedArray) != 1 {
				t.Fatalf("streamed Value.array = %#v (%T), want one-element []interface{}", streamedValue["array"], streamedValue["array"])
			}
			assertNumber("streamed Value.integer", streamedValue["integer"], 1)
			assertNumber("streamed Value.array[0]", streamedArray[0], 2)
		})
	}
}

func TestUpstreamPerformanceReviewSearcherOptions(t *testing.T) {
	const malformedSelected = `{"selected":{garbage}}`
	if _, err := ast.NewSearcher(malformedSelected).GetByPath("selected"); err == nil {
		t.Fatal("ValidateJSON=true accepted malformed selected value")
	}
	if _, err := ast.NewSearcher(`{garbage}`).GetByPath(); err == nil {
		t.Fatal("ValidateJSON=true accepted malformed root value")
	}

	withoutValidation := ast.NewSearcher(malformedSelected)
	withoutValidation.ValidateJSON = false
	rawNode, err := withoutValidation.GetByPath("selected")
	if err != nil {
		t.Fatalf("ValidateJSON=false GetByPath error = %v", err)
	}
	if got, err := rawNode.Raw(); err != nil || got != `{garbage}` {
		t.Fatalf("ValidateJSON=false Raw() = %q, %v; want {garbage}, nil", got, err)
	}

	withoutValidation = ast.NewSearcher(`{garbage}`)
	withoutValidation.ValidateJSON = false
	rawNode, err = withoutValidation.GetByPath()
	if err != nil {
		t.Fatalf("ValidateJSON=false root GetByPath error = %v", err)
	}
	if got, err := rawNode.Raw(); err != nil || got != `{garbage}` {
		t.Fatalf("ValidateJSON=false root Raw() = %q, %v; want {garbage}, nil", got, err)
	}

	for _, tt := range []struct {
		name string
		src  string
		path []interface{}
		want string
	}{
		{name: "root string", src: `"\q"`, want: `"\q"`},
		{name: "root object", src: `{"a":"\q","b":1}`, want: `{"a":"\q","b":1}`},
		{name: "path string", src: `{"a":"\q","b":1}`, path: []interface{}{"a"}, want: `"\q"`},
	} {
		for _, validate := range []bool{true, false} {
			t.Run(tt.name, func(t *testing.T) {
				searcher := ast.NewSearcher(tt.src)
				searcher.ValidateJSON = validate
				node, err := searcher.GetByPath(tt.path...)
				if err != nil {
					t.Fatalf("ValidateJSON=%t GetByPath(%q, %v) error = %v", validate, tt.src, tt.path, err)
				}
				if got, err := node.Raw(); err != nil || got != tt.want {
					t.Fatalf("ValidateJSON=%t Raw() = %q, %v; want %q, nil", validate, got, err, tt.want)
				}
			})
		}
	}

	if _, err := ast.NewSearcher(`{"a":"\q","broken":garbage}`).GetByPath(); err == nil {
		t.Fatal("ValidateJSON=true accepted malformed container after invalid escape")
	}

	for _, src := range []string{
		`{"before":{garbage},"selected":1}`,
		`{"selected":1,"after":{garbage}}`,
	} {
		node, err := ast.NewSearcher(src).GetByPath("selected")
		if err != nil {
			t.Fatalf("GetByPath selected with malformed sibling %q error = %v", src, err)
		}
		if got, err := node.Int64(); err != nil || got != 1 {
			t.Fatalf("GetByPath selected with malformed sibling %q = %d, %v; want 1, nil", src, got, err)
		}
	}

	newMutableSource := func() ([]byte, string) {
		backing := []byte(`{"selected":"old"}`)
		return backing, unsafe.String(unsafe.SliceData(backing), len(backing))
	}

	backing, source := newMutableSource()
	aliased, err := ast.NewSearcher(source).GetByPath("selected")
	if err != nil {
		t.Fatalf("CopyReturn=false GetByPath error = %v", err)
	}
	index := bytes.Index(backing, []byte("old"))
	if index < 0 {
		t.Fatal("mutable source does not contain old")
	}
	backing[index] = 'n'
	if got, err := aliased.Raw(); err != nil || got != `"nld"` {
		t.Fatalf("CopyReturn=false Raw() after source mutation = %q, %v; want \"nld\", nil", got, err)
	}

	backing, source = newMutableSource()
	copiedSearcher := ast.NewSearcher(source)
	copiedSearcher.CopyReturn = true
	copied, err := copiedSearcher.GetByPath("selected")
	if err != nil {
		t.Fatalf("CopyReturn=true GetByPath error = %v", err)
	}
	index = bytes.Index(backing, []byte("old"))
	backing[index] = 'n'
	if got, err := copied.Raw(); err != nil || got != `"old"` {
		t.Fatalf("CopyReturn=true Raw() after source mutation = %q, %v; want \"old\", nil", got, err)
	}
}

func TestUpstreamPerformanceReviewConcurrentRead(t *testing.T) {
	searcher := ast.NewSearcher(`{"target":{"integer":1,"float":1.5,"number":2,"bool":true,"string":"sonic","array":[1],"map":{"key":1},"interface":{"key":1}}}`)
	searcher.ConcurrentRead = true
	node, err := searcher.GetByPath("target")
	if err != nil {
		t.Fatalf("GetByPath target error = %v", err)
	}

	readAll := func() error {
		get := func(key string) (*ast.Node, error) {
			child := node.GetByPath(key)
			if child == nil {
				return nil, fmt.Errorf("GetByPath(%q) = nil", key)
			}
			if err := child.Check(); err != nil {
				return nil, fmt.Errorf("GetByPath(%q) Check error = %w", key, err)
			}
			return child, nil
		}
		integer, err := get("integer")
		if err != nil {
			return err
		}
		if got, err := integer.Int64(); err != nil || got != 1 {
			return fmt.Errorf("Int64() = %d, %v; want 1, nil", got, err)
		}
		floating, err := get("float")
		if err != nil {
			return err
		}
		if got, err := floating.Float64(); err != nil || got != 1.5 {
			return fmt.Errorf("Float64() = %v, %v; want 1.5, nil", got, err)
		}
		number, err := get("number")
		if err != nil {
			return err
		}
		if got, err := number.Number(); err != nil || got != "2" {
			return fmt.Errorf("Number() = %q, %v; want 2, nil", got, err)
		}
		boolean, err := get("bool")
		if err != nil {
			return err
		}
		if got, err := boolean.Bool(); err != nil || !got {
			return fmt.Errorf("Bool() = %v, %v; want true, nil", got, err)
		}
		stringNode, err := get("string")
		if err != nil {
			return err
		}
		if got, err := stringNode.String(); err != nil || got != "sonic" {
			return fmt.Errorf("String() = %q, %v; want sonic, nil", got, err)
		}
		array, err := get("array")
		if err != nil {
			return err
		}
		if got, err := array.Array(); err != nil || len(got) != 1 {
			return fmt.Errorf("Array() = %#v, %v; want one element, nil", got, err)
		}
		object, err := get("map")
		if err != nil {
			return err
		}
		if got, err := object.Map(); err != nil || got["key"] != float64(1) {
			return fmt.Errorf("Map() = %#v, %v; want key 1, nil", got, err)
		}
		value, err := get("interface")
		if err != nil {
			return err
		}
		if got, err := value.Interface(); err != nil || got.(map[string]interface{})["key"] != float64(1) {
			return fmt.Errorf("Interface() = %#v, %v; want key 1, nil", got, err)
		}
		if got, err := node.Raw(); err != nil || got == "" {
			return fmt.Errorf("Raw() = %q, %v; want non-empty raw JSON, nil", got, err)
		}
		if got, err := node.MarshalJSON(); err != nil || !json.Valid(got) {
			return fmt.Errorf("MarshalJSON() = %q, %v; want valid JSON, nil", got, err)
		}
		return nil
	}

	const goroutines = 32
	const iterations = 16
	start := make(chan struct{})
	errorsByWorker := make(chan error, goroutines)
	var workers sync.WaitGroup
	workers.Add(goroutines)
	for range goroutines {
		go func() {
			defer workers.Done()
			<-start
			for range iterations {
				if err := readAll(); err != nil {
					errorsByWorker <- err
					return
				}
			}
		}()
	}
	close(start)
	workers.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		t.Error(err)
	}
}

type upstreamPerformanceReviewVisitor struct {
	upstreamSkipVisitor
	beginErr error
	keyErr   error
}

func (v *upstreamPerformanceReviewVisitor) OnObjectBegin(int) error  { return v.beginErr }
func (v *upstreamPerformanceReviewVisitor) OnObjectKey(string) error { return v.keyErr }

func TestUpstreamPerformanceReviewVisitOPSkipIdentity(t *testing.T) {
	wrapped := fmt.Errorf("wrapped skip: %w", ast.VisitOPSkip)
	if err := ast.Preorder(`{"key":1}`, &upstreamPerformanceReviewVisitor{beginErr: wrapped}, nil); err != wrapped {
		t.Fatalf("wrapped OnObjectBegin error = %v, want original wrapped error", err)
	}
	if err := ast.Preorder(`{"key":1}`, &upstreamPerformanceReviewVisitor{keyErr: ast.VisitOPSkip}, nil); err != ast.VisitOPSkip {
		t.Fatalf("OnObjectKey VisitOPSkip error = %v, want VisitOPSkip", err)
	}
}

func TestUpstreamPerformanceReviewNodeEdges(t *testing.T) {
	object := ast.NewObject(nil)
	if _, err := object.SetAny("value", 1); err != nil {
		t.Fatalf("SetAny error = %v", err)
	}
	if got := object.Get("value"); got == nil || got.Type() != ast.V_ANY {
		t.Fatalf("SetAny child type = %v, want V_ANY", got)
	}

	array := ast.NewArray(nil)
	if err := array.AddAny(1); err != nil {
		t.Fatalf("AddAny error = %v", err)
	}
	if got := array.Index(0); got == nil || got.Type() != ast.V_ANY {
		t.Fatalf("AddAny child type = %v, want V_ANY", got)
	}

	indexed := ast.NewArray([]ast.Node{ast.NewNull()})
	if _, err := indexed.SetAnyByIndex(0, 1); err != nil {
		t.Fatalf("SetAnyByIndex error = %v", err)
	}
	if got := indexed.Index(0); got == nil || got.Type() != ast.V_ANY {
		t.Fatalf("SetAnyByIndex child type = %v, want V_ANY", got)
	}
	if _, err := indexed.UnsetByIndex(1); !errors.Is(err, ast.ErrNotExist) {
		t.Fatalf("UnsetByIndex out of range error = %v, want ErrNotExist", err)
	}

	for _, empty := range []*ast.Node{
		func() *ast.Node { node := ast.NewArray(nil); return &node }(),
		func() *ast.Node { node := ast.NewObject(nil); return &node }(),
	} {
		if err := empty.Pop(); err != nil {
			t.Fatalf("empty Pop error = %v, want nil", err)
		}
	}

	sorted := ast.NewArray([]ast.Node{ast.NewObject([]ast.Pair{
		ast.NewPair("b", ast.NewNumber("2")),
		ast.NewPair("a", ast.NewNumber("1")),
	})})
	if err := sorted.SortKeys(false); err != nil {
		t.Fatalf("array SortKeys(false) error = %v", err)
	}
	if pair := sorted.Index(0).IndexPair(0); pair == nil || pair.Key != "a" {
		t.Fatalf("array SortKeys(false) first child pair = %#v, want key a", pair)
	}

	unsupported, gotIndex := sorted.IndexOrGetWithIdx(7, "ignored")
	if gotIndex != 7 {
		t.Fatalf("array IndexOrGetWithIdx index = %d, want requested 7", gotIndex)
	}
	if unsupported == nil || unsupported.Type() != ast.V_ERROR {
		t.Fatalf("array IndexOrGetWithIdx node = %#v, want unsupported V_ERROR", unsupported)
	}

	var zero ast.Node
	if got, err := zero.Len(); err != nil || got != 0 {
		t.Fatalf("zero Node Len = %d, %v; want 0, nil", got, err)
	}
	if _, err := zero.MarshalJSON(); !errors.Is(err, ast.ErrNotExist) {
		t.Fatalf("zero Node MarshalJSON error = %v, want ErrNotExist", err)
	}
	callbacks := 0
	if err := zero.ForEach(func(path ast.Sequence, node *ast.Node) bool {
		callbacks++
		if path.Index != -1 || node != &zero {
			t.Errorf("zero Node ForEach callback = %#v, %p; want index -1 and original node", path, node)
		}
		return true
	}); err != nil {
		t.Fatalf("zero Node ForEach error = %v", err)
	}
	if callbacks != 1 {
		t.Fatalf("zero Node ForEach callbacks = %d, want 1", callbacks)
	}
}

// TestUpstreamSortKeysFalseTraversesNestedArrays records the v1.15.2 behavior
// shared by the Go 1.26.7 native and Go 1.27 fallback implementations: an
// array receiver always crosses array containers to reach objects, while false
// still prevents recursion below those object containers.
func TestUpstreamSortKeysFalseTraversesNestedArrays(t *testing.T) {
	node := ast.NewArray([]ast.Node{ast.NewArray([]ast.Node{ast.NewObject([]ast.Pair{
		ast.NewPair("d", ast.NewNumber("4")),
		ast.NewPair("c", ast.NewNumber("3")),
	})})})
	if err := node.SortKeys(false); err != nil {
		t.Fatalf("SortKeys(false) error = %v", err)
	}
	if got, err := node.Raw(); err != nil || got != `[[{"c":3,"d":4}]]` {
		t.Fatalf("SortKeys(false) raw = %q, %v; want nested-array object keys sorted", got, err)
	}
}

// TestUpstreamSortKeysMaterializesLazyContainers records the v1.15.2 behavior
// shared by its Go 1.26.7 native and Go 1.27 fallback implementations.
func TestUpstreamSortKeysMaterializesLazyContainers(t *testing.T) {
	for _, tt := range []struct {
		name    string
		node    ast.Node
		recurse bool
		want    string
	}{
		{
			name:    "false lazy object child",
			node:    ast.NewArray([]ast.Node{ast.NewRaw(`{"b":2,"a":1}`)}),
			want:    `[{"a":1,"b":2}]`,
			recurse: false,
		},
		{
			name:    "false lazy nested array child",
			node:    ast.NewArray([]ast.Node{ast.NewRaw(`[{"d":4,"c":3}]`)}),
			want:    `[[{"c":3,"d":4}]]`,
			recurse: false,
		},
		{
			name: "true lazy object descendant",
			node: ast.NewObject([]ast.Pair{
				ast.NewPair("outer", ast.NewRaw(`{"d":4,"c":3}`)),
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

func TestUpstreamPerformanceReviewErrorDescriptions(t *testing.T) {
	var nilMessage *sonic.NoCopyRawMessage
	if err := nilMessage.UnmarshalJSON([]byte(`null`)); err == nil || err.Error() != "sonic.NoCopyRawMessage: UnmarshalJSON on nil pointer" {
		t.Fatalf("nil NoCopyRawMessage.UnmarshalJSON error = %v, want exact upstream error", err)
	}

	if version.Compare(runtime.Version(), "go1.27") < 0 {
		syntaxValue := reflect.New(reflect.TypeOf(decoder.SyntaxError{})).Elem()
		syntaxValue.FieldByName("Pos").SetInt(0)
		syntaxValue.FieldByName("Src").SetString("x")
		syntaxValue.FieldByName("Msg").SetString("custom syntax message")
		syntax := syntaxValue.Interface().(decoder.SyntaxError)
		if got, want := syntax.Description(), "Syntax error at index 0: custom syntax message\n\n\tx\n\t^\n"; got != want {
			t.Fatalf("SyntaxError.Description() = %q, want %q", got, want)
		}

		mismatchValue := reflect.New(reflect.TypeOf(decoder.MismatchTypeError{})).Elem()
		mismatchValue.FieldByName("Pos").SetInt(0)
		mismatchValue.FieldByName("Src").SetString("1")
		mismatchValue.FieldByName("Type").Set(reflect.ValueOf(reflect.TypeOf("")))
		mismatch := mismatchValue.Interface().(decoder.MismatchTypeError)
		description := reflect.ValueOf(mismatch).MethodByName("Description")
		if !description.IsValid() {
			t.Fatal("native MismatchTypeError has no Description")
		}
		if got, want := description.Call(nil)[0].String(), "Mismatch type string with value number at index 0: mismatched type with value\n\n\t1\n\t^\n"; got != want {
			t.Fatalf("MismatchTypeError.Description() = %q, want %q", got, want)
		}
	} else {
		syntaxValue := reflect.New(reflect.TypeOf(decoder.SyntaxError{})).Elem()
		syntaxValue.FieldByName("Offset").SetInt(1)
		syntax := syntaxValue.Interface().(decoder.SyntaxError)
		if got := syntax.Description(); got != "" {
			t.Fatalf("fallback SyntaxError.Description() = %q, want empty zero-value description", got)
		}
		if _, ok := reflect.TypeOf(decoder.MismatchTypeError{}).MethodByName("Description"); ok {
			t.Fatal("fallback MismatchTypeError unexpectedly has Description")
		}
	}
}
