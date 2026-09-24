package decoder

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

var _ interface {
	Decode(any) error
	Buffered() io.Reader
	InputOffset() int64
	More() bool
	Pos() int
	Reset(string)
	SetOptions(Options)
	UseInt64()
	UseNumber()
	UseUnicodeErrors()
	DisallowUnknownFields()
	CopyString()
	ValidateString()
	CheckTrailings() error
} = (*StreamDecoder)(nil)

func TestNativeStreamOptionsAndOffsets(t *testing.T) {
	d := NewStreamDecoder(strings.NewReader("  1 \t2\n 3 4"))
	var v any
	d.UseNumber()
	if err := d.Decode(&v); err != nil || v != json.Number("1") {
		t.Fatalf("number=%#v %v", v, err)
	}
	if d.Pos() != 1 || d.InputOffset() != 5 {
		t.Fatalf("positions=%d/%d", d.Pos(), d.InputOffset())
	}
	if b, _ := io.ReadAll(d.Buffered()); string(b) != "2\n 3 4" {
		t.Fatalf("buffer=%q", b)
	}
	d.SetOptions(0)
	if err := d.Decode(&v); err != nil || v != float64(2) {
		t.Fatalf("disable number=%#v %v", v, err)
	}
	d.UseInt64()
	if err := d.Decode(&v); err != nil || v != int64(3) {
		t.Fatalf("int64=%#v %v", v, err)
	}
	d.UseNumber()
	if err := d.Decode(&v); err != nil || v != json.Number("4") {
		t.Fatalf("number after int64=%#v %v", v, err)
	}
	if d.Pos() != 1 || d.InputOffset() != 11 {
		t.Fatalf("final positions=%d/%d", d.Pos(), d.InputOffset())
	}
	if err := d.CheckTrailings(); err != nil {
		t.Fatal(err)
	}
}

type oneByteReader struct{ r *strings.Reader }

func (r oneByteReader) Read(p []byte) (int, error) { return r.r.Read(p[:1]) }
func TestNativeStreamOptionsWithChunkedInput(t *testing.T) {
	d := NewStreamDecoder(oneByteReader{strings.NewReader(" \n1  2")})
	var n int
	for _, want := range []int{1, 2} {
		if err := d.Decode(&n); err != nil || n != want || d.Pos() != 1 {
			t.Fatalf("chunk=%d pos=%d err=%v", n, d.Pos(), err)
		}
	}
}

var nativeStreamFailure = errors.New("custom stream failure")

type nativeFailingValue int

func (*nativeFailingValue) UnmarshalJSON([]byte) error { return nativeStreamFailure }
func TestNativeStreamErrorsAreTerminal(t *testing.T) {
	d := NewStreamDecoder(strings.NewReader(`1 2`))
	var v nativeFailingValue
	if err := d.Decode(&v); err != nativeStreamFailure {
		t.Fatal(err)
	}
	var n int
	if err := d.Decode(&n); err != nativeStreamFailure || n != 0 || d.More() {
		t.Fatalf("terminal error=%v n=%d more=%v", err, n, d.More())
	}
	if b, _ := io.ReadAll(d.Buffered()); len(b) != 0 {
		t.Fatalf("terminal buffered=%q", b)
	}
	d = NewStreamDecoder(strings.NewReader(`{"unknown":1} 2`))
	d.DisallowUnknownFields()
	var out struct{}
	first := d.Decode(&out)
	if first == nil || d.Decode(&n) != first {
		t.Fatal("unknown-field error was not retained")
	}
}

func TestStringDecoderOptionChangesPreserveCursor(t *testing.T) {
	d := NewDecoder(`1 2 3`)
	var v any
	if err := d.Decode(&v); err != nil || reflect.TypeOf(v) != reflect.TypeOf(float64(0)) {
		t.Fatalf("default=%#v %v", v, err)
	}
	d.UseNumber()
	if err := d.Decode(&v); err != nil || v != json.Number("2") || d.Pos() != 3 {
		t.Fatalf("number=%#v pos=%d %v", v, d.Pos(), err)
	}
	d.UseInt64()
	if err := d.Decode(&v); err != nil || v != int64(3) || d.Pos() != 5 {
		t.Fatalf("int64=%#v pos=%d %v", v, d.Pos(), err)
	}
}

func BenchmarkStringDecoderSequential(b *testing.B) {
	d := NewDecoder(`1 2 3 4 5 6 7 8`)
	var n int
	b.ReportAllocs()
	for b.Loop() {
		d.Reset(`1 2 3 4 5 6 7 8`)
		for range 8 {
			if err := d.Decode(&n); err != nil {
				b.Fatal(err)
			}
		}
	}
}
func BenchmarkStreamDecoderSequential(b *testing.B) {
	src := strings.Repeat("1 ", 1000)
	var n int
	b.ReportAllocs()
	for b.Loop() {
		d := NewStreamDecoder(strings.NewReader(src))
		for range 1000 {
			if err := d.Decode(&n); err != nil {
				b.Fatal(err)
			}
		}
	}
}
