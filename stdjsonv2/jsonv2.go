//go:build goexperiment.jsonv2

// Real implementation of the stdjsonv2 API using encoding/json/v2 and
// encoding/json/jsontext. Selected when the toolchain is built with
// GOEXPERIMENT=jsonv2.

package stdjsonv2

import (
	"bytes"
	"encoding/json"
	stdjson "encoding/json"
	stdjsontext "encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"

	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/internal/fastjsoncompat"
	"github.com/bytedance/sonic/internal/jsonconv"
)

// ErrJSONv2ExperimentDisabled is declared for API symmetry with the
// non-jsonv2 build. Under GOEXPERIMENT=jsonv2 it is never returned by any
// operation; it exists so callers that reference the error continue to
// compile regardless of build configuration.
var ErrJSONv2ExperimentDisabled = errors.New("stdjsonv2: GOEXPERIMENT=jsonv2 is not enabled")

// froze returns the jsonv2-backed API. It is the build-specific
// implementation of (Config).Froze declared in api.go.
func froze(cfg Config) API {
	return &jsonv2API{
		cfg:           cfg,
		marshalOpts:   buildMarshalOptions(cfg),
		unmarshalOpts: buildUnmarshalOptions(cfg),
	}
}

// doGet is the build-specific implementation of Get/GetFromString/
// GetCopyFromString declared in api.go. It delegates to GetWithOptions.
func doGet(data []byte, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	return GetWithOptions(data, opts, path...)
}

// GetWithOptions resolves path with explicit search options.
//
// It validates the input document when opts.ValidateJSON is set, then uses
// ast.NewRaw(string(src)) + Searcher.GetByPath to resolve the path. The
// use of ast.NewRaw (not ast.NewBytes) matches the task contract: the
// searcher parses raw JSON text on demand.
func GetWithOptions(src []byte, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	if len(src) == 0 || !stdjsontext.Value(src).IsValid(stdjsontext.AllowDuplicateNames(true), stdjsontext.AllowInvalidUTF8(true)) {
		return ast.Node{}, &ast.SyntaxError{Src: string(src), Msg: "invalid JSON value"}
	}
	opts.ValidateJSON = false
	return fastjsoncompat.Get(src, opts, path...)
}

// jsonv2API is the jsonv2-backed implementation of API.
type jsonv2API struct {
	cfg           Config
	marshalOpts   []jsonv2.Options
	unmarshalOpts []jsonv2.Options
}

func (a *jsonv2API) Marshal(v interface{}) ([]byte, error) {
	return jsonv2.Marshal(v, a.marshalOpts...)
}

func (a *jsonv2API) MarshalToString(v interface{}) (string, error) {
	b, err := a.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (a *jsonv2API) MarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	// jsontext.WithIndent/WithIndentPrefix panic when the strings contain
	// anything other than space/tab, while sonic's MarshalIndent (and
	// encoding/json) accept arbitrary prefix strings. Marshal compactly,
	// then re-indent via stdjson.Indent which accepts any characters.
	b, err := jsonv2.Marshal(v, a.marshalOpts...)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := stdjson.Indent(&buf, b, prefix, indent); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (a *jsonv2API) UnmarshalFromString(buf string, val interface{}) error {
	return a.Unmarshal([]byte(buf), val)
}

func (a *jsonv2API) Unmarshal(data []byte, val interface{}) error {
	return decodeBytes(data, val, a.cfg, a.unmarshalOpts)
}

func (a *jsonv2API) Valid(data []byte) bool {
	// AllowDuplicateNames/AllowInvalidUTF8 match RFC 8259 syntax (and
	// the root backend): duplicate object names and invalid UTF-8 are
	// accepted by standard JSON and must not be rejected here.
	return len(data) > 0 && stdjsontext.Value(data).IsValid(stdjsontext.AllowDuplicateNames(true), stdjsontext.AllowInvalidUTF8(true))
}

func (a *jsonv2API) NewEncoder(w io.Writer) Encoder {
	return &jsonv2Encoder{
		w:          w,
		cfg:        a.cfg,
		escapeHTML: a.cfg.EscapeHTML,
		noNewline:  a.cfg.NoEncoderNewline,
	}
}

func (a *jsonv2API) NewDecoder(r io.Reader) Decoder {
	// AllowDuplicateNames/AllowInvalidUTF8 keep RFC 8259 syntax
	// acceptance (duplicate object names and invalid UTF-8 are valid
	// JSON per the standard and accepted by the root backend).
	dec := stdjsontext.NewDecoder(r, stdjsontext.AllowDuplicateNames(true), stdjsontext.AllowInvalidUTF8(true))
	return &jsonv2Decoder{
		dec:                   dec,
		cfg:                   a.cfg,
		unmarshalOpts:         buildUnmarshalOptions(a.cfg),
		useNumber:             a.cfg.UseNumber,
		disallowUnknownFields: a.cfg.DisallowUnknownFields,
	}
}

// buildMarshalOptions builds the jsonv2 Options slice for a Marshal call.
func buildMarshalOptions(cfg Config) []jsonv2.Options {
	opts := []jsonv2.Options{jsonv2.DefaultOptionsV2()}
	opts = append(opts, stdjsontext.EscapeForHTML(cfg.EscapeHTML))
	if cfg.SortMapKeys {
		opts = append(opts, jsonv2.Deterministic(true))
	}
	// Sonic's default (NoNullSliceOrMap=false) encodes nil slices/maps
	// as null like encoding/json; jsonv2's default emits []/{}. Force
	// the null form unless the caller opted into []/{}.
	if !cfg.NoNullSliceOrMap {
		opts = append(opts, jsonv2.FormatNilSliceAsNull(true))
		opts = append(opts, jsonv2.FormatNilMapAsNull(true))
	}
	return opts
}

// buildUnmarshalOptions builds the jsonv2 Options slice for an Unmarshal call.
func buildUnmarshalOptions(cfg Config) []jsonv2.Options {
	opts := []jsonv2.Options{jsonv2.DefaultOptionsV2()}
	if cfg.DisallowUnknownFields {
		opts = append(opts, jsonv2.RejectUnknownMembers(true))
	}
	// Sonic's default (CaseSensitive=false) matches keys
	// case-insensitively like encoding/json v1; jsonv2's default is
	// case-sensitive. Enable insensitive matching unless the caller
	// opted into strict case matching.
	if !cfg.CaseSensitive {
		opts = append(opts, jsonv2.MatchCaseInsensitiveNames(true))
	}
	return opts
}

// decodeBytes decodes data into val. When cfg.UseNumber is set we use the
// standard encoding/json.Decoder so that callers see json.Number values
// (jsonv2 v2 semantics decode numbers as float64 by default and do not
// expose a UseNumber knob on the v2 API). Otherwise we use jsonv2.Unmarshal
// to honor the v2-specific options (RejectUnknownMembers, etc.).
func decodeBytes(data []byte, val interface{}, cfg Config, opts []jsonv2.Options) error {
	if cfg.UseNumber || cfg.UseInt64 {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if cfg.DisallowUnknownFields {
			dec.DisallowUnknownFields()
		}
		if err := dec.Decode(val); err != nil {
			return err
		}
		if err := rejectTrailingStdJSONData(dec); err != nil {
			return err
		}
		if cfg.UseInt64 && !cfg.UseNumber {
			jsonconv.ConvertNumbersToInt64(val)
		}
		return nil
	}
	// AllowDuplicateNames/AllowInvalidUTF8 keep RFC 8259 syntax
	// acceptance (both are valid standard JSON and accepted by the root
	// backend and by encoding/json v1).
	allOpts := make([]jsonv2.Options, 0, len(opts)+2)
	allOpts = append(allOpts, opts...)
	allOpts = append(allOpts, stdjsontext.AllowDuplicateNames(true), stdjsontext.AllowInvalidUTF8(true))
	return jsonv2.Unmarshal(data, val, allOpts...)
}

var errTrailingStdJSONData = errors.New("invalid trailing data after top-level value")

func rejectTrailingStdJSONData(dec *json.Decoder) error {
	var extra struct{}
	err := dec.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return errTrailingStdJSONData
	}
	return err
}

// jsonv2Encoder implements Encoder by buffering each value and writing
// it out with short-write retry. It does not wrap a jsontext.Encoder
// directly because jsontext.Flush silently drops short writes, which
// would violate the documented io.ErrShortWrite retry contract.
type jsonv2Encoder struct {
	w            io.Writer
	cfg          Config
	escapeHTML   bool
	indent       string
	indentPrefix string
	noNewline    bool
}

// encodeOptions returns the jsonv2 options for one Encode call.
func (e *jsonv2Encoder) encodeOptions() []jsonv2.Options {
	opts := []jsonv2.Options{
		stdjsontext.EscapeForHTML(e.escapeHTML),
	}
	if e.cfg.SortMapKeys {
		opts = append(opts, jsonv2.Deterministic(true))
	}
	// Sonic's default (NoNullSliceOrMap=false) encodes nil slices/maps
	// as null; jsonv2's default emits []/{}.
	if !e.cfg.NoNullSliceOrMap {
		opts = append(opts, jsonv2.FormatNilSliceAsNull(true))
		opts = append(opts, jsonv2.FormatNilMapAsNull(true))
	}
	return opts
}

func (e *jsonv2Encoder) Encode(v interface{}) error {
	b, err := jsonv2.Marshal(v, e.encodeOptions()...)
	if err != nil {
		return err
	}
	if e.indent != "" || e.indentPrefix != "" {
		// json.Indent accepts arbitrary prefix/indent characters; the
		// jsontext WithIndent options panic on non-space characters.
		var buf bytes.Buffer
		if err := stdjson.Indent(&buf, b, e.indentPrefix, e.indent); err != nil {
			return err
		}
		b = buf.Bytes()
	}
	if !e.noNewline {
		b = append(b, '\n')
	}
	return writeAll(e.w, b)
}

func (e *jsonv2Encoder) SetEscapeHTML(on bool) {
	e.escapeHTML = on
}

func (e *jsonv2Encoder) SetIndent(prefix, indent string) {
	e.indentPrefix = prefix
	e.indent = indent
}

// writeAll writes p to w, retrying short writes until the whole buffer
// is written. A write that makes no progress returns io.ErrShortWrite.
func writeAll(w io.Writer, p []byte) error {
	for offset := 0; offset < len(p); {
		n, err := w.Write(p[offset:])
		if err != nil {
			return err
		}
		if n <= 0 || n > len(p)-offset {
			return io.ErrShortWrite
		}
		offset += n
	}
	return nil
}

// jsonv2Decoder implements Decoder by wrapping a jsontext.Decoder.
type jsonv2Decoder struct {
	dec                   *stdjsontext.Decoder
	cfg                   Config
	unmarshalOpts         []jsonv2.Options
	useNumber             bool
	disallowUnknownFields bool
}

func (d *jsonv2Decoder) Decode(v interface{}) error {
	// Read the next raw JSON value from the stream and decode it through
	// decodeBytes so per-call options (UseNumber, DisallowUnknownFields)
	// apply consistently.
	val, err := d.dec.ReadValue()
	if err != nil {
		return err
	}
	return decodeBytes([]byte(val), v, d.cfg, d.unmarshalOpts)
}

func (d *jsonv2Decoder) Buffered() io.Reader {
	// Return a copy of the unread buffer so the reader remains valid
	// after subsequent Decoder calls.
	if b := d.dec.UnreadBuffer(); len(b) > 0 {
		return bytes.NewReader(append([]byte(nil), b...))
	}
	return bytes.NewReader(nil)
}

func (d *jsonv2Decoder) DisallowUnknownFields() {
	d.disallowUnknownFields = true
	d.cfg.DisallowUnknownFields = true
	d.unmarshalOpts = buildUnmarshalOptions(d.cfg)
}

func (d *jsonv2Decoder) More() bool {
	// PeekKind returns KindInvalid (0) at EOF or end-of-stream; any other
	// kind means there is another value to read.
	return d.dec.PeekKind() != stdjsontext.KindInvalid
}

func (d *jsonv2Decoder) UseNumber() {
	d.useNumber = true
	d.cfg.UseNumber = true
	d.cfg.UseInt64 = false
}

// Compile-time interface satisfaction checks.
var (
	_ API     = (*jsonv2API)(nil)
	_ Encoder = (*jsonv2Encoder)(nil)
	_ Decoder = (*jsonv2Decoder)(nil)
)
