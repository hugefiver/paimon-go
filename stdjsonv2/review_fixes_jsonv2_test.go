//go:build goexperiment.jsonv2

package stdjsonv2

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

// Regression tests for issues found in the full-codebase review.

// MarshalIndent must accept arbitrary prefix/indent characters (like
// sonic and encoding/json); jsontext.WithIndent panics on non-space
// characters, so the implementation must route through json.Indent.
func TestMarshalIndentAcceptsArbitraryPrefix(t *testing.T) {
	out, err := ConfigDefault.MarshalIndent(map[string]int{"a": 1}, "PFX", "IX")
	if err != nil {
		t.Fatalf("MarshalIndent error = %v", err)
	}
	// json.Indent applies prefix at every newline and indent per depth:
	// {\nPFXIX"a": 1\nPFX}
	want := "{\nPFXIX\"a\": 1\nPFX}"
	if string(out) != want {
		t.Fatalf("MarshalIndent output = %q; want %q", out, want)
	}
}

// SetIndent with non-space characters must not panic either.
func TestEncoderSetIndentArbitraryPrefixNoPanic(t *testing.T) {
	var buf bytes.Buffer
	enc := ConfigDefault.NewEncoder(&buf)
	enc.SetIndent("PFX", "IX")
	if err := enc.Encode(map[string]int{"a": 1}); err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if !strings.Contains(buf.String(), "\nPFX") {
		t.Fatalf("encoder output = %s; want PFX prefix", buf.String())
	}
}

// shortWriter accepts n bytes per call; the encoder must retry until
// the whole payload is written (docs/compatibility.md contract).
type shortWriter struct {
	dst     bytes.Buffer
	perCall int
	calls   int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	w.calls++
	n := w.perCall
	if n <= 0 || n > len(p) {
		n = len(p)
	}
	return w.dst.Write(p[:n])
}

func TestEncoderRetriesShortWrites(t *testing.T) {
	w := &shortWriter{perCall: 3}
	enc := ConfigDefault.NewEncoder(w)
	if err := enc.Encode(map[string]int{"abc": 123}); err != nil {
		t.Fatalf("Encode error = %v", err)
	}
	if w.calls < 2 {
		t.Fatalf("Encode used %d Write calls; short writes must be retried", w.calls)
	}
	if !strings.HasSuffix(w.dst.String(), "\n") {
		t.Fatalf("output missing trailing newline: %q", w.dst.String())
	}
}

// zeroWriter makes no progress; Encode must return io.ErrShortWrite.
type zeroWriter struct{}

func (zeroWriter) Write(p []byte) (int, error) { return 0, nil }

func TestEncoderZeroProgressReturnsErrShortWrite(t *testing.T) {
	enc := ConfigDefault.NewEncoder(zeroWriter{})
	if err := enc.Encode(map[string]int{"a": 1}); err != io.ErrShortWrite {
		t.Fatalf("Encode error = %v; want io.ErrShortWrite", err)
	}
}

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestEncoderPropagatesWriterError(t *testing.T) {
	want := errors.New("write boom")
	enc := ConfigDefault.NewEncoder(errorWriter{err: want})
	if err := enc.Encode(map[string]int{"a": 1}); !errors.Is(err, want) {
		t.Fatalf("Encode error = %v; want errors.Is(_, %v)", err, want)
	}
}

// NoNullSliceOrMap defaults to false: nil slices/maps encode as null
// (Sonic/encoding/json semantics), not as []/{}.
func TestDefaultConfigEncodesNilCollectionsAsNull(t *testing.T) {
	type nils struct {
		Slice []string       `json:"slice"`
		Map   map[string]int `json:"map"`
	}
	out, err := ConfigDefault.Marshal(nils{})
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	if string(out) != `{"slice":null,"map":null}` {
		t.Fatalf("Marshal(nils) = %s; want null collections", out)
	}
}

// CaseSensitive defaults to false: key matching is case-insensitive
// like encoding/json v1 and Sonic's default.
func TestDefaultConfigMatchesKeysCaseInsensitively(t *testing.T) {
	var v struct {
		Known string `json:"known"`
	}
	if err := ConfigDefault.Unmarshal([]byte(`{"KNOWN":"a"}`), &v); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if v.Known != "a" {
		t.Fatalf("Known = %q; want \"a\" (case-insensitive match)", v.Known)
	}
}

func TestUseNumberTakesPrecedenceOverUseInt64(t *testing.T) {
	api := Config{UseNumber: true, UseInt64: true}.Froze()

	var nested map[string]interface{}
	if err := api.Unmarshal([]byte(`{"n":1,"a":[2]}`), &nested); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
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

	dec := api.NewDecoder(strings.NewReader(`1`))
	var streamed interface{}
	if err := dec.Decode(&streamed); err != nil {
		t.Fatalf("stream Decode error = %v", err)
	}
	if got, ok := streamed.(json.Number); !ok || got.String() != "1" {
		t.Fatalf("streamed = %v (%T), want json.Number(1)", streamed, streamed)
	}
}

func TestDecoderUseNumberTakesPrecedenceOverFrozenUseInt64(t *testing.T) {
	for _, tt := range []struct {
		name string
		cfg  Config
	}{
		{name: "use int64", cfg: Config{UseInt64: true}},
		{name: "both modes", cfg: Config{UseNumber: true, UseInt64: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dec := tt.cfg.Froze().NewDecoder(strings.NewReader(`{"n":1}`))
			dec.UseNumber()

			var got map[string]interface{}
			if err := dec.Decode(&got); err != nil {
				t.Fatalf("Decode error = %v", err)
			}
			if number, ok := got["n"].(json.Number); !ok || number.String() != "1" {
				t.Fatalf("n = %v (%T), want json.Number(1)", got["n"], got["n"])
			}
		})
	}
}

// Duplicate object names and invalid UTF-8 are valid JSON per RFC 8259
// and must be accepted (root-backend parity).
func TestValidAcceptsDuplicateNamesAndInvalidUTF8(t *testing.T) {
	if !ConfigDefault.Valid([]byte(`{"a":1,"a":2}`)) {
		t.Fatalf("Valid(duplicate names) = false; want true")
	}
	if !ConfigDefault.Valid([]byte("\"a\xffb\"")) {
		t.Fatalf("Valid(invalid UTF-8) = false; want true")
	}
}

func TestUnmarshalAcceptsDuplicateNames(t *testing.T) {
	var v struct {
		A int `json:"a"`
	}
	if err := ConfigDefault.Unmarshal([]byte(`{"a":1,"a":2}`), &v); err != nil {
		t.Fatalf("Unmarshal(duplicate names) error = %v", err)
	}
}

func TestGetAcceptsDuplicateNames(t *testing.T) {
	node, err := Get([]byte(`{"a":1,"a":2}`), "a")
	if err != nil {
		t.Fatalf("Get(duplicate names) error = %v", err)
	}
	if got, _ := node.Int64(); got != 1 {
		t.Fatalf("Get(duplicate names) = %v; want 1", got)
	}
}
