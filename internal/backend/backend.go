// Package backend defines the shared interfaces and configuration mirror
// used by the Sonic replacement's pluggable backends.
//
// A backend implements JSON marshalling, unmarshalling, validation, path
// lookup, and streaming encode/decode against a normalized Config. The root
// sonic package wires a concrete backend implementation behind the public
// API surface; later tasks swap fastjson and stdjsonv2 implementations in
// through this contract.
package backend

import (
	"io"

	"github.com/bytedance/sonic/ast"
)

// Config is the backend-visible mirror of sonic.Config. It carries the
// same option fields as the public Config so backends need not depend on
// the root package (which would create an import cycle). The root package
// converts sonic.Config to this type before invoking a Backend.
type Config struct {
	EscapeHTML              bool
	SortMapKeys             bool
	CompactMarshaler        bool
	NoQuoteTextMarshaler    bool
	NoNullSliceOrMap        bool
	UseInt64                bool
	UseNumber               bool
	UseUnicodeErrors        bool
	DisallowUnknownFields   bool
	CopyString              bool
	ValidateString          bool
	NoValidateJSONMarshaler bool
	NoValidateJSONSkip      bool
	NoEncoderNewline        bool
	EncodeNullForInfOrNan   bool
	CaseSensitive           bool
}

// Backend is the contract every JSON engine must satisfy. Implementations
// are expected to be safe for concurrent use; per-call state lives in the
// returned StreamEncoder/StreamDecoder values.
type Backend interface {
	// Marshal serializes v as JSON under cfg.
	Marshal(v interface{}, cfg Config) ([]byte, error)
	// MarshalIndent is like Marshal but applies a two-dimensional indent.
	MarshalIndent(v interface{}, prefix, indent string, cfg Config) ([]byte, error)
	// Unmarshal parses data into v under cfg.
	Unmarshal(data []byte, v interface{}, cfg Config) error
	// Valid reports whether data is a single well-formed JSON value.
	Valid(data []byte) bool
	// Get resolves path against data and returns the matching node.
	Get(data []byte, opts ast.SearchOptions, path ...interface{}) (ast.Node, error)
	// NewEncoder returns a streaming JSON encoder writing to w.
	NewEncoder(w io.Writer, cfg Config) StreamEncoder
	// NewDecoder returns a streaming JSON decoder reading from r.
	NewDecoder(r io.Reader, cfg Config) StreamDecoder
}

// StreamEncoder is the streaming encoder contract. It mirrors the subset
// of encoding/json's json.Encoder API that sonic exposes, plus the
// SetIndent knob.
type StreamEncoder interface {
	Encode(v interface{}) error
	SetEscapeHTML(on bool)
	SetIndent(prefix, indent string)
}

// StreamDecoder is the streaming decoder contract. It mirrors the subset
// of encoding/json's json.Decoder API that sonic exposes.
type StreamDecoder interface {
	Decode(v interface{}) error
	Buffered() io.Reader
	DisallowUnknownFields()
	More() bool
	UseNumber()
}
