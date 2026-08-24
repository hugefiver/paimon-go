package upstream_test

import (
	"bytes"
	"encoding/json"
	"testing"

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
