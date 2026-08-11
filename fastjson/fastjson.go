// Package fastjson is an explicit backend subpackage that exposes the
// high-frequency API and types of the root sonic package under the
// github.com/bytedance/sonic/fastjson import path.
//
// It is a thin wrapper layer: every exported type is a type alias for the
// corresponding root package type, and every exported function forwards to
// the root package implementation. Callers that import this subpackage get
// the exact same behavior as callers that import the root sonic package.
//
// The package exists so that downstream code (and automated migrations)
// referencing github.com/bytedance/sonic/fastjson keep compiling and
// behaving correctly when the underlying engine is the Sonic-compatible
// replacement provided by this module.
package fastjson

import (
	"io"
	"reflect"

	sonic "github.com/bytedance/sonic"
	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/option"
)

// Type aliases expose the root package's public types under the fastjson
// import path. Because these are true type aliases, values produced by
// either package are interchangeable without conversion.
type (
	// Config is an alias for sonic.Config.
	Config = sonic.Config
	// API is an alias for sonic.API.
	API = sonic.API
	// Encoder is an alias for sonic.Encoder.
	Encoder = sonic.Encoder
	// Decoder is an alias for sonic.Decoder.
	Decoder = sonic.Decoder
	// NoCopyRawMessage is an alias for sonic.NoCopyRawMessage.
	NoCopyRawMessage = sonic.NoCopyRawMessage
)

// Pre-configured API instances. They are aliases for the root package's
// variables so callers that swap imports see the identical engine.
var (
	ConfigDefault = sonic.ConfigDefault
	ConfigStd     = sonic.ConfigStd
	ConfigFastest = sonic.ConfigFastest
)

// Marshal serializes v as JSON using ConfigDefault.
func Marshal(v interface{}) ([]byte, error) { return sonic.Marshal(v) }

// MarshalString is like Marshal but returns a string.
func MarshalString(v interface{}) (string, error) { return sonic.MarshalString(v) }

// MarshalIndent serializes v with a two-dimensional indent.
func MarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	return sonic.MarshalIndent(v, prefix, indent)
}

// Unmarshal parses data into v using ConfigDefault.
func Unmarshal(data []byte, v interface{}) error { return sonic.Unmarshal(data, v) }

// UnmarshalString parses a string into v using ConfigDefault.
func UnmarshalString(buf string, v interface{}) error { return sonic.UnmarshalString(buf, v) }

// Valid reports whether data is a single well-formed JSON value.
func Valid(data []byte) bool { return sonic.Valid(data) }

// ValidString is the string-input form of Valid.
func ValidString(data string) bool { return sonic.ValidString(data) }

// Get resolves path against data and returns the matching AST node.
func Get(data []byte, path ...interface{}) (ast.Node, error) { return sonic.Get(data, path...) }

// GetFromString is the string-input form of Get.
func GetFromString(data string, path ...interface{}) (ast.Node, error) {
	return sonic.GetFromString(data, path...)
}

// GetCopyFromString is like GetFromString but returns a node that is safe
// to retain.
func GetCopyFromString(data string, path ...interface{}) (ast.Node, error) {
	return sonic.GetCopyFromString(data, path...)
}

// GetWithOptions resolves path with explicit search options.
func GetWithOptions(data []byte, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	return sonic.GetWithOptions(data, opts, path...)
}

// NewEncoder returns a streaming JSON encoder writing to w using
// ConfigDefault. The root sonic package does not expose a package-level
// NewEncoder, so this subpackage provides one that forwards to
// ConfigDefault.NewEncoder.
func NewEncoder(w io.Writer) Encoder { return ConfigDefault.NewEncoder(w) }

// NewDecoder returns a streaming JSON decoder reading from r using
// ConfigDefault. The root sonic package does not expose a package-level
// NewDecoder, so this subpackage provides one that forwards to
// ConfigDefault.NewDecoder.
func NewDecoder(r io.Reader) Decoder { return ConfigDefault.NewDecoder(r) }

// Pretouch precompiles the given type. It is a no-op in this phase and
// returns nil; the variadic option.CompileOption argument is accepted for
// source compatibility with Sonic's public API.
func Pretouch(t reflect.Type, opts ...option.CompileOption) error {
	return sonic.Pretouch(t, opts...)
}

// PretouchMany is the variadic form of Pretouch.
func PretouchMany(ts []reflect.Type, opts ...option.CompileOption) error {
	return sonic.PretouchMany(ts, opts...)
}
