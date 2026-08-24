// Package sonic is a drop-in replacement for github.com/bytedance/sonic
// v1.15.2. This phase exposes the full public API surface (Config, API,
// Encoder, Decoder, package-level helpers, NoCopyRawMessage, Pretouch)
// backed by an internal reflection fallback. Later tasks swap in
// fastjson-based and stdjsonv2-based backends behind the same interface.
//
// The root API is intentionally a thin façade over a backend.Backend so
// the public surface stays stable as engines change.
package sonic

import (
	"io"
	"reflect"

	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/internal/backend"
	"github.com/bytedance/sonic/internal/fastjsoncompat"
	"github.com/bytedance/sonic/internal/stdjsoncompat"
	"github.com/bytedance/sonic/option"
)

// Backend selection constants. The library always selects UseSonicJSON in
// this phase; UseStdJSON is exposed for API compatibility.
const (
	UseStdJSON = iota
	UseSonicJSON
	APIKind = UseSonicJSON
)

// Config tunes marshalling, unmarshalling, validation, and streaming for
// an API instance. Fields mirror Sonic v1.15.2 exactly. The zero value is
// a valid, permissive configuration.
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

// API is the frozen (immutable) entry point produced by Config.Froze.
// Methods on API honor the originating Config.
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

// NoCopyRawMessage is a json.RawMessage analogue that never copies. Its
// UnmarshalJSON assigns the input slice directly, and MarshalJSON returns
// the underlying bytes without copying. This matches Sonic v1.15.2's
// NoCopyRawMessage semantics for callers that own the input buffer.
type NoCopyRawMessage []byte

// MarshalJSON returns the underlying bytes. No copy is performed. A nil
// message is encoded as the JSON null literal.
func (m NoCopyRawMessage) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	return m, nil
}

// UnmarshalJSON assigns data directly to m without copying. The caller
// must not mutate data afterward unless m is detached first.
func (m *NoCopyRawMessage) UnmarshalJSON(data []byte) error {
	if m == nil {
		return errNoCopyRawMessageNil
	}
	*m = data
	return nil
}

// errNoCopyRawMessageNil is returned when UnmarshalJSON is called on a
// nil NoCopyRawMessage pointer.
var errNoCopyRawMessageNil = newError("sonic: UnmarshalJSON on nil pointer of NoCopyRawMessage")

// newError is a tiny helper to avoid importing fmt in the hot path.
func newError(msg string) error { return &stringError{msg: msg} }

type stringError struct{ msg string }

func (e *stringError) Error() string { return e.msg }

// api is the concrete API implementation. It holds an immutable Config
// snapshot and the backend selected for this configuration.
type api struct {
	cfg  Config
	bknd backend.Backend
}

// Froze freezes the configuration into an immutable API instance. The
// returned API honors every field of the originating Config.
func (cfg Config) Froze() API {
	normalized := cfg.toBackend()
	return &api{
		cfg:  cfg,
		bknd: newBackend(normalized),
	}
}

// toBackend converts the public Config to the backend.Config mirror.
func (cfg Config) toBackend() backend.Config {
	return backend.Config{
		EscapeHTML:              cfg.EscapeHTML,
		SortMapKeys:             cfg.SortMapKeys,
		CompactMarshaler:        cfg.CompactMarshaler,
		NoQuoteTextMarshaler:    cfg.NoQuoteTextMarshaler,
		NoNullSliceOrMap:        cfg.NoNullSliceOrMap,
		UseInt64:                cfg.UseInt64,
		UseNumber:               cfg.UseNumber,
		UseUnicodeErrors:        cfg.UseUnicodeErrors,
		DisallowUnknownFields:   cfg.DisallowUnknownFields,
		CopyString:              cfg.CopyString,
		ValidateString:          cfg.ValidateString,
		NoValidateJSONMarshaler: cfg.NoValidateJSONMarshaler,
		NoValidateJSONSkip:      cfg.NoValidateJSONSkip,
		NoEncoderNewline:        cfg.NoEncoderNewline,
		EncodeNullForInfOrNan:   cfg.EncodeNullForInfOrNan,
		CaseSensitive:           cfg.CaseSensitive,
	}
}

// ---------------------------------------------------------------------------
// API method implementations
// ---------------------------------------------------------------------------

func (a *api) MarshalToString(v interface{}) (string, error) {
	b, err := a.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (a *api) Marshal(v interface{}) ([]byte, error) {
	return a.bknd.Marshal(v, a.cfg.toBackend())
}

func (a *api) MarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	return a.bknd.MarshalIndent(v, prefix, indent, a.cfg.toBackend())
}

func (a *api) UnmarshalFromString(buf string, val interface{}) error {
	return a.bknd.Unmarshal([]byte(buf), val, a.cfg.toBackend())
}

func (a *api) Unmarshal(buf []byte, val interface{}) error {
	return a.bknd.Unmarshal(buf, val, a.cfg.toBackend())
}

func (a *api) NewEncoder(writer io.Writer) Encoder {
	return a.bknd.NewEncoder(writer, a.cfg.toBackend())
}

func (a *api) NewDecoder(reader io.Reader) Decoder {
	return a.bknd.NewDecoder(reader, a.cfg.toBackend())
}

func (a *api) Valid(data []byte) bool {
	return a.bknd.Valid(data)
}

// ---------------------------------------------------------------------------
// defaultBackend routes operations to the stdjsoncompat fallback and
// fastjsoncompat helpers. This is the reflection-based engine that is
// always available.
// ---------------------------------------------------------------------------

type defaultBackend struct{}

func (defaultBackend) Marshal(v interface{}, cfg backend.Config) ([]byte, error) {
	return stdjsoncompat.Marshal(v, cfg)
}

func (defaultBackend) MarshalIndent(v interface{}, prefix, indent string, cfg backend.Config) ([]byte, error) {
	return stdjsoncompat.MarshalIndent(v, prefix, indent, cfg)
}

func (defaultBackend) Unmarshal(data []byte, v interface{}, cfg backend.Config) error {
	return stdjsoncompat.Unmarshal(data, v, cfg)
}

func (defaultBackend) Valid(data []byte) bool {
	return fastjsoncompat.Valid(data)
}

func (defaultBackend) Get(data []byte, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	return fastjsoncompat.Get(data, opts, path...)
}

func (defaultBackend) NewEncoder(w io.Writer, cfg backend.Config) backend.StreamEncoder {
	return stdjsoncompat.NewEncoder(w, cfg)
}

func (defaultBackend) NewDecoder(r io.Reader, cfg backend.Config) backend.StreamDecoder {
	return stdjsoncompat.NewDecoder(r, cfg)
}

// ---------------------------------------------------------------------------
// Pre-configured API instances
// ---------------------------------------------------------------------------

// ConfigDefault is the permissive default configuration matching Sonic's
// zero-value Config.
var ConfigDefault = Config{}.Froze()

// ConfigStd mirrors Sonic's ConfigStd: HTML escaping, sorted map keys,
// compact marshaler output, defensive string copying, and validation
// enabled.
var ConfigStd = Config{
	EscapeHTML:       true,
	SortMapKeys:      true,
	CompactMarshaler: true,
	CopyString:       true,
	ValidateString:   true,
}.Froze()

// ConfigFastest mirrors Sonic's ConfigFastest: validation of JSON
// marshalers and skip markers is disabled for maximum throughput.
var ConfigFastest = Config{
	NoValidateJSONMarshaler: true,
	NoValidateJSONSkip:      true,
}.Froze()

// ---------------------------------------------------------------------------
// Package-level convenience functions
// ---------------------------------------------------------------------------

// Marshal serializes v as JSON using ConfigDefault.
func Marshal(v interface{}) ([]byte, error) {
	return ConfigDefault.Marshal(v)
}

// MarshalString is like Marshal but returns a string.
func MarshalString(v interface{}) (string, error) {
	return ConfigDefault.MarshalToString(v)
}

// MarshalIndent serializes v with a two-dimensional indent.
func MarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	return ConfigDefault.MarshalIndent(v, prefix, indent)
}

// Unmarshal parses data into v using ConfigDefault.
func Unmarshal(data []byte, v interface{}) error {
	return ConfigDefault.Unmarshal(data, v)
}

// UnmarshalString parses a string into v using ConfigDefault.
func UnmarshalString(buf string, v interface{}) error {
	return ConfigDefault.UnmarshalFromString(buf, v)
}

// Valid reports whether data is a single well-formed JSON value.
func Valid(data []byte) bool {
	return ConfigDefault.Valid(data)
}

// ValidString is the string-input form of Valid.
func ValidString(data string) bool {
	return Valid([]byte(data))
}

// Get resolves path against data and returns the matching AST node using
// the build-selected searcher.
func Get(data []byte, path ...interface{}) (ast.Node, error) {
	return selectedGet(data, ast.SearchOptions{}, path...)
}

// GetFromString is the string-input form of Get.
func GetFromString(data string, path ...interface{}) (ast.Node, error) {
	return selectedGet([]byte(data), ast.SearchOptions{}, path...)
}

// GetCopyFromString is like GetFromString but returns a node that is safe
// to retain.
func GetCopyFromString(data string, path ...interface{}) (ast.Node, error) {
	return selectedGet([]byte(data), ast.SearchOptions{CopyReturn: true}, path...)
}

// GetWithOptions resolves path with explicit search options.
func GetWithOptions(data []byte, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	return selectedGet(data, opts, path...)
}

// Pretouch precompiles the given type. In this phase it is a no-op that
// returns nil; later tasks will populate the JIT compiler cache.
func Pretouch(_ reflect.Type, _ ...option.CompileOption) error {
	return nil
}

// PretouchMany is the variadic form of Pretouch.
func PretouchMany(_ []reflect.Type, _ ...option.CompileOption) error {
	return nil
}
