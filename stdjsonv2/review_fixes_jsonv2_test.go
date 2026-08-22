//go:build goexperiment.jsonv2

package stdjsonv2

import (
	"bytes"
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

// UseNumber+UseInt64 conflict panics like the root API (Sonic
// compatibility documented in docs/compatibility.md).
func TestUseNumberAndUseInt64ConflictPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("Config{UseNumber,UseInt64}.Froze() did not panic")
		}
	}()
	Config{UseNumber: true, UseInt64: true}.Froze()
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
