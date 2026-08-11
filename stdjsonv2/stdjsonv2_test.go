//go:build !goexperiment.jsonv2

// Tests for the default (stub) build of stdjsonv2. Without
// GOEXPERIMENT=jsonv2 every operation must return the deterministic
// disabled error (or a safe no-op value) so callers can detect the
// absence of the jsonv2 backend at runtime.

package stdjsonv2

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/bytedance/sonic/ast"
)

// Compile-time assertions that the pre-configured instances and the
// NewEncoder/NewDecoder helpers satisfy the public interfaces.
var (
	_ API     = ConfigDefault
	_ API     = ConfigStd
	_ API     = ConfigFastest
	_ Encoder = NewEncoder(&bytes.Buffer{})
	_ Decoder = NewDecoder(strings.NewReader("{}"))
)

func TestStubMarshalReturnsDisabledError(t *testing.T) {
	b, err := Marshal(map[string]int{"a": 1})
	if !errors.Is(err, ErrJSONv2ExperimentDisabled) {
		t.Fatalf("Marshal err = %v, want ErrJSONv2ExperimentDisabled", err)
	}
	if b != nil {
		t.Fatalf("Marshal bytes = %v, want nil", b)
	}
}

func TestStubMarshalStringReturnsDisabledError(t *testing.T) {
	s, err := MarshalString(map[string]int{"a": 1})
	if !errors.Is(err, ErrJSONv2ExperimentDisabled) {
		t.Fatalf("MarshalString err = %v", err)
	}
	if s != "" {
		t.Fatalf("MarshalString s = %q, want empty", s)
	}
}

func TestStubMarshalIndentReturnsDisabledError(t *testing.T) {
	b, err := MarshalIndent(map[string]int{"a": 1}, "", "  ")
	if !errors.Is(err, ErrJSONv2ExperimentDisabled) {
		t.Fatalf("MarshalIndent err = %v", err)
	}
	if b != nil {
		t.Fatalf("MarshalIndent bytes = %v, want nil", b)
	}
}

func TestStubUnmarshalReturnsDisabledError(t *testing.T) {
	var v map[string]int
	err := Unmarshal([]byte(`{"a":1}`), &v)
	if !errors.Is(err, ErrJSONv2ExperimentDisabled) {
		t.Fatalf("Unmarshal err = %v", err)
	}
}

func TestStubUnmarshalStringReturnsDisabledError(t *testing.T) {
	var v map[string]int
	err := UnmarshalString(`{"a":1}`, &v)
	if !errors.Is(err, ErrJSONv2ExperimentDisabled) {
		t.Fatalf("UnmarshalString err = %v", err)
	}
}

func TestStubGetReturnsDisabledError(t *testing.T) {
	n, err := Get([]byte(`{"a":1}`), "a")
	if !errors.Is(err, ErrJSONv2ExperimentDisabled) {
		t.Fatalf("Get err = %v", err)
	}
	_ = n
}

func TestStubGetFromStringReturnsDisabledError(t *testing.T) {
	n, err := GetFromString(`{"a":1}`, "a")
	if !errors.Is(err, ErrJSONv2ExperimentDisabled) {
		t.Fatalf("GetFromString err = %v", err)
	}
	_ = n
}

func TestStubGetCopyFromStringReturnsDisabledError(t *testing.T) {
	n, err := GetCopyFromString(`{"a":1}`, "a")
	if !errors.Is(err, ErrJSONv2ExperimentDisabled) {
		t.Fatalf("GetCopyFromString err = %v", err)
	}
	_ = n
}

func TestStubGetWithOptionsReturnsDisabledError(t *testing.T) {
	n, err := GetWithOptions([]byte(`{"a":1}`), ast.SearchOptions{ValidateJSON: true}, "a")
	if !errors.Is(err, ErrJSONv2ExperimentDisabled) {
		t.Fatalf("GetWithOptions err = %v", err)
	}
	_ = n
}

func TestStubValidReturnsFalse(t *testing.T) {
	if Valid([]byte(`{"a":1}`)) {
		t.Fatalf("Valid returned true, want false")
	}
	if ValidString(`{"a":1}`) {
		t.Fatalf("ValidString returned true, want false")
	}
}

func TestStubNewEncoderEncodeReturnsDisabled(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	// Setters should be no-ops.
	enc.SetEscapeHTML(true)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]string{"a": "b"}); !errors.Is(err, ErrJSONv2ExperimentDisabled) {
		t.Fatalf("Encode err = %v, want ErrJSONv2ExperimentDisabled", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("buf = %q, want empty", buf.String())
	}
}

func TestStubNewDecoderDeterministic(t *testing.T) {
	dec := NewDecoder(strings.NewReader(`{"a":1}`))
	// Setters should be no-ops.
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if dec.More() {
		t.Fatalf("More returned true, want false")
	}
	// Buffered should return a non-nil readable reader.
	r := dec.Buffered()
	if r == nil {
		t.Fatalf("Buffered returned nil")
	}
	// The reader should yield no bytes.
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll(Buffered()) err = %v", err)
	}
	if len(b) != 0 {
		t.Fatalf("Buffered() returned %d bytes, want 0", len(b))
	}
	var v map[string]int
	if err := dec.Decode(&v); !errors.Is(err, ErrJSONv2ExperimentDisabled) {
		t.Fatalf("Decode err = %v, want ErrJSONv2ExperimentDisabled", err)
	}
}

func TestStubConfigFrozeReturnsDisabledAPI(t *testing.T) {
	api := Config{EscapeHTML: true, SortMapKeys: true}.Froze()
	b, err := api.Marshal(map[string]int{"a": 1})
	if !errors.Is(err, ErrJSONv2ExperimentDisabled) {
		t.Fatalf("Froze().Marshal err = %v", err)
	}
	if b != nil {
		t.Fatalf("Froze().Marshal bytes = %v, want nil", b)
	}
	if api.Valid([]byte(`{}`)) {
		t.Fatalf("Froze().Valid returned true")
	}
}
