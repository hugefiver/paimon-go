package stdjsoncompat

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/bytedance/sonic/internal/backend"
)

type indentCountingMarshaler struct {
	calls int
}

func (m *indentCountingMarshaler) MarshalJSON() ([]byte, error) {
	m.calls++
	return []byte(`{"value":"<tag>"}`), nil
}

func TestMarshalIndentCallsMarshalerOnceWithoutHTMLEscape(t *testing.T) {
	stdlibValue := &indentCountingMarshaler{}
	if _, err := json.MarshalIndent(stdlibValue, "prefix:", "  "); err != nil {
		t.Fatalf("json.MarshalIndent error = %v", err)
	}
	if stdlibValue.calls != 1 {
		t.Fatalf("json.MarshalIndent calls = %d, want 1", stdlibValue.calls)
	}

	referenceValue := &indentCountingMarshaler{}
	var reference bytes.Buffer
	enc := json.NewEncoder(&reference)
	enc.SetEscapeHTML(false)
	enc.SetIndent("prefix:", "  ")
	if err := enc.Encode(referenceValue); err != nil {
		t.Fatalf("reference Encode error = %v", err)
	}
	referenceBytes := bytes.TrimSuffix(reference.Bytes(), []byte{'\n'})
	if referenceValue.calls != 1 {
		t.Fatalf("reference Encode calls = %d, want 1", referenceValue.calls)
	}

	value := &indentCountingMarshaler{}
	got, err := MarshalIndent(value, "prefix:", "  ", backend.Config{EscapeHTML: false})
	if err != nil {
		t.Fatalf("MarshalIndent error = %v", err)
	}
	if value.calls != 1 {
		t.Fatalf("MarshalJSON calls = %d, want 1", value.calls)
	}
	if !bytes.Equal(got, referenceBytes) {
		t.Fatalf("MarshalIndent output = %s, want %s", got, referenceBytes)
	}
	if !bytes.Contains(got, []byte(`<tag>`)) {
		t.Fatalf("MarshalIndent output = %s, want literal <tag>", got)
	}
	if bytes.HasSuffix(got, []byte{'\n'}) {
		t.Fatalf("MarshalIndent output has trailing newline: %q", got)
	}
}
