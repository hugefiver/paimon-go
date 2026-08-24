// Package stdjsonv2 is an explicit backend subpackage that exposes the
// Sonic v1.15.2 high-frequency JSON API backed by encoding/json/v2 when the
// goexperiment.jsonv2 build constraint is enabled. On Go 1.27, jsonv2 is an
// ambient default, so an unset GOEXPERIMENT uses the real backend; explicit
// GOEXPERIMENT=none selects the deterministic disabled stub, while explicit
// GOEXPERIMENT=jsonv2 also selects the real backend.
//
// The public API surface (Config, API, Encoder, Decoder, and the
// package-level helpers) is declared in this file and shared across build
// configurations. The concrete behavior is provided by:
//
//   - stub.go  (//go:build !goexperiment.jsonv2): every operation returns a
//     deterministic "experiment disabled" error so callers can detect the
//     absence of the jsonv2 backend at runtime.
//   - jsonv2.go (//go:build goexperiment.jsonv2): a real implementation
//     using encoding/json/v2 and encoding/json/jsontext.
//
// Because the public types and package-level functions are declared once
// here, the build-specific files MUST NOT redeclare them. They only
// declare the private types/implementations behind those interfaces and
// (where needed) a small number of build-specific helpers.
package stdjsonv2

import (
	"io"
	"reflect"

	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/option"
)

// Config tunes marshalling, unmarshalling, validation, and streaming for
// an API instance. Fields mirror sonic.Config (and thus Sonic v1.15.2)
// exactly so callers can swap imports without field changes. The zero
// value is a valid, permissive configuration.
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

// API is the frozen (immutable) entry point produced by Config.Froze. It
// mirrors the root sonic.API surface; methods honor the originating
// Config.
type API interface {
	MarshalToString(v interface{}) (string, error)
	Marshal(v interface{}) ([]byte, error)
	MarshalIndent(v interface{}, prefix, indent string) ([]byte, error)
	UnmarshalFromString(buf string, val interface{}) error
	Unmarshal(buf []byte, val interface{}) error
	NewEncoder(writer io.Writer) Encoder
	NewDecoder(reader io.Reader) Decoder
	Valid(data []byte) bool
}

// Encoder is the streaming encoder contract exposed by API.NewEncoder.
type Encoder interface {
	Encode(v interface{}) error
	SetEscapeHTML(on bool)
	SetIndent(prefix, indent string)
}

// Decoder is the streaming decoder contract exposed by API.NewDecoder.
type Decoder interface {
	Decode(v interface{}) error
	Buffered() io.Reader
	DisallowUnknownFields()
	More() bool
	UseNumber()
}

// Froze freezes the configuration into an immutable API instance. The concrete
// implementation is selected by the goexperiment.jsonv2 build constraint. On
// Go 1.27, an unset GOEXPERIMENT uses the jsonv2-backed API; explicit
// GOEXPERIMENT=none returns an API whose every operation fails with
// ErrJSONv2ExperimentDisabled, while explicit GOEXPERIMENT=jsonv2 uses the
// jsonv2-backed API.
func (cfg Config) Froze() API { return froze(cfg) }

// Pre-configured API instances. They mirror the root package's
// ConfigDefault/ConfigStd/ConfigFastest.
var (
	ConfigDefault = Config{}.Froze()
	ConfigStd     = Config{
		EscapeHTML:       true,
		SortMapKeys:      true,
		CompactMarshaler: true,
		CopyString:       true,
		ValidateString:   true,
	}.Froze()
	ConfigFastest = Config{
		NoValidateJSONMarshaler: true,
		NoValidateJSONSkip:      true,
	}.Froze()
)

// Marshal serializes v as JSON using ConfigDefault.
func Marshal(v interface{}) ([]byte, error) { return ConfigDefault.Marshal(v) }

// MarshalString is like Marshal but returns a string.
func MarshalString(v interface{}) (string, error) { return ConfigDefault.MarshalToString(v) }

// MarshalIndent serializes v with a two-dimensional indent using
// ConfigDefault.
func MarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	return ConfigDefault.MarshalIndent(v, prefix, indent)
}

// Unmarshal parses data into v using ConfigDefault.
func Unmarshal(data []byte, v interface{}) error { return ConfigDefault.Unmarshal(data, v) }

// UnmarshalString parses a string into v using ConfigDefault.
func UnmarshalString(buf string, v interface{}) error {
	return ConfigDefault.UnmarshalFromString(buf, v)
}

// Valid reports whether data is a single well-formed JSON value.
func Valid(data []byte) bool { return ConfigDefault.Valid(data) }

// ValidString is the string-input form of Valid.
func ValidString(data string) bool { return Valid([]byte(data)) }

// NewEncoder returns a streaming JSON encoder writing to w using
// ConfigDefault.
func NewEncoder(w io.Writer) Encoder { return ConfigDefault.NewEncoder(w) }

// NewDecoder returns a streaming JSON decoder reading from r using
// ConfigDefault.
func NewDecoder(r io.Reader) Decoder { return ConfigDefault.NewDecoder(r) }

// Pretouch precompiles the given type. It is a no-op in this phase and
// returns nil; the variadic option.CompileOption argument is accepted for
// source compatibility with Sonic's public API.
func Pretouch(_ reflect.Type, _ ...option.CompileOption) error { return nil }

// PretouchMany is the variadic form of Pretouch.
func PretouchMany(_ []reflect.Type, _ ...option.CompileOption) error { return nil }

// Get resolves path against data and returns the matching AST node.
func Get(data []byte, path ...interface{}) (ast.Node, error) {
	return doGet(data, ast.SearchOptions{}, path...)
}

// GetFromString is the string-input form of Get.
func GetFromString(data string, path ...interface{}) (ast.Node, error) {
	return doGet([]byte(data), ast.SearchOptions{}, path...)
}

// GetCopyFromString is like GetFromString but returns a node that is safe
// to retain.
func GetCopyFromString(data string, path ...interface{}) (ast.Node, error) {
	return doGet([]byte(data), ast.SearchOptions{CopyReturn: true}, path...)
}
