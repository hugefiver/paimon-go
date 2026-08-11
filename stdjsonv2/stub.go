//go:build !goexperiment.jsonv2

// Stub implementation used when the toolchain was not built with
// GOEXPERIMENT=jsonv2. Every operation returns a deterministic
// "experiment disabled" error (or a safe no-op value for boolean/reader
// returns) so callers can detect the absence of the jsonv2 backend at
// runtime.

package stdjsonv2

import (
	"bytes"
	"errors"
	"io"

	"github.com/bytedance/sonic/ast"
)

// ErrJSONv2ExperimentDisabled is returned by every operation on the
// stdjsonv2 API when the toolchain was not built with
// GOEXPERIMENT=jsonv2. It allows callers to detect the absence of the
// jsonv2 backend at runtime and fall back to another engine.
var ErrJSONv2ExperimentDisabled = errors.New("stdjsonv2: GOEXPERIMENT=jsonv2 is not enabled; build with a toolchain that has the jsonv2 experiment enabled")

// froze returns the disabled API. It is the build-specific implementation
// of (Config).Froze declared in api.go.
func froze(cfg Config) API { return &disabledAPI{cfg: cfg} }

// doGet is the build-specific implementation of Get/GetFromString/
// GetCopyFromString declared in api.go. Without jsonv2 it always returns
// the disabled error.
func doGet(_ []byte, _ ast.SearchOptions, _ ...interface{}) (ast.Node, error) {
	return ast.Node{}, ErrJSONv2ExperimentDisabled
}

// GetWithOptions resolves path with explicit search options. Without
// jsonv2 it always returns the disabled error.
func GetWithOptions(_ []byte, _ ast.SearchOptions, _ ...interface{}) (ast.Node, error) {
	return ast.Node{}, ErrJSONv2ExperimentDisabled
}

// disabledAPI implements API by failing every operation.
type disabledAPI struct{ cfg Config }

func (a *disabledAPI) MarshalToString(interface{}) (string, error) {
	return "", ErrJSONv2ExperimentDisabled
}
func (a *disabledAPI) Marshal(interface{}) ([]byte, error) {
	return nil, ErrJSONv2ExperimentDisabled
}
func (a *disabledAPI) MarshalIndent(interface{}, string, string) ([]byte, error) {
	return nil, ErrJSONv2ExperimentDisabled
}
func (a *disabledAPI) UnmarshalFromString(string, interface{}) error {
	return ErrJSONv2ExperimentDisabled
}
func (a *disabledAPI) Unmarshal([]byte, interface{}) error { return ErrJSONv2ExperimentDisabled }
func (a *disabledAPI) NewEncoder(io.Writer) Encoder        { return &disabledEncoder{} }
func (a *disabledAPI) NewDecoder(io.Reader) Decoder        { return &disabledDecoder{} }
func (a *disabledAPI) Valid([]byte) bool                   { return false }

// disabledEncoder implements Encoder by failing Encode and no-op-ing setters.
type disabledEncoder struct{}

func (*disabledEncoder) Encode(interface{}) error { return ErrJSONv2ExperimentDisabled }
func (*disabledEncoder) SetEscapeHTML(bool)       {}
func (*disabledEncoder) SetIndent(string, string) {}

// disabledDecoder implements Decoder by failing Decode, returning false
// for More, a readable empty reader for Buffered, and no-op-ing setters.
type disabledDecoder struct{}

func (*disabledDecoder) Decode(interface{}) error { return ErrJSONv2ExperimentDisabled }
func (*disabledDecoder) Buffered() io.Reader      { return bytes.NewReader(nil) }
func (*disabledDecoder) DisallowUnknownFields()   {}
func (*disabledDecoder) More() bool               { return false }
func (*disabledDecoder) UseNumber()               {}
