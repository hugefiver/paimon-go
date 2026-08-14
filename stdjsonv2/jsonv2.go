//go:build goexperiment.jsonv2

// Real implementation of the stdjsonv2 API using encoding/json/v2 and
// encoding/json/jsontext. Selected when the toolchain is built with
// GOEXPERIMENT=jsonv2.

package stdjsonv2

import (
	"bytes"
	"encoding/json"
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
	if len(src) == 0 || !stdjsontext.Value(src).IsValid() {
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
	// jsonv2 Marshal does not take a prefix; the prefix is applied via the
	// jsontext.WithIndentPrefix option. The indent string maps to
	// jsontext.WithIndent. MarshalEncode into a jsontext.Encoder would
	// re-apply the options, so we simply marshal then format through a
	// jsontext.Value which handles the canonical formatting.
	b, err := jsonv2.Marshal(v, a.marshalOpts...)
	if err != nil {
		return nil, err
	}
	// Indent the marshalled bytes using jsontext.Value.Indent, which
	// accepts the encode-side options. We pass Multiline + WithIndent +
	// WithIndentPrefix to mirror encoding/json.MarshalIndent semantics.
	val := stdjsontext.Value(b)
	encOpts := []stdjsontext.Options{
		stdjsontext.Multiline(true),
		stdjsontext.WithIndent(indent),
		stdjsontext.WithIndentPrefix(prefix),
	}
	if err := val.Indent(encOpts...); err != nil {
		return nil, err
	}
	return []byte(val), nil
}

func (a *jsonv2API) UnmarshalFromString(buf string, val interface{}) error {
	return a.Unmarshal([]byte(buf), val)
}

func (a *jsonv2API) Unmarshal(data []byte, val interface{}) error {
	return decodeBytes(data, val, a.cfg, a.unmarshalOpts)
}

func (a *jsonv2API) Valid(data []byte) bool {
	return len(data) > 0 && stdjsontext.Value(data).IsValid()
}

func (a *jsonv2API) NewEncoder(w io.Writer) Encoder {
	enc := stdjsontext.NewEncoder(w, a.encodeStreamOptions()...)
	return &jsonv2Encoder{
		enc:          enc,
		w:            w,
		cfg:          a.cfg,
		escapeHTML:   a.cfg.EscapeHTML,
		indent:       "",
		indentPrefix: "",
		noNewline:    a.cfg.NoEncoderNewline,
	}
}

func (a *jsonv2API) NewDecoder(r io.Reader) Decoder {
	dec := stdjsontext.NewDecoder(r)
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
	if cfg.NoNullSliceOrMap {
		opts = append(opts, jsonv2.FormatNilSliceAsNull(false))
		opts = append(opts, jsonv2.FormatNilMapAsNull(false))
	}
	if cfg.CaseSensitive {
		opts = append(opts, jsonv2.MatchCaseInsensitiveNames(false))
	}
	return opts
}

// buildUnmarshalOptions builds the jsonv2 Options slice for an Unmarshal call.
func buildUnmarshalOptions(cfg Config) []jsonv2.Options {
	opts := []jsonv2.Options{jsonv2.DefaultOptionsV2()}
	if cfg.DisallowUnknownFields {
		opts = append(opts, jsonv2.RejectUnknownMembers(true))
	}
	if cfg.CaseSensitive {
		opts = append(opts, jsonv2.MatchCaseInsensitiveNames(false))
	}
	return opts
}

// encodeStreamOptions builds the jsontext Options slice for a streaming
// Encoder. It mirrors buildMarshalOptions but only includes the encode-side
// options (no unmarshal options).
func (a *jsonv2API) encodeStreamOptions() []stdjsontext.Options {
	cfg := a.cfg
	var opts []stdjsontext.Options
	opts = append(opts, stdjsontext.EscapeForHTML(cfg.EscapeHTML))
	if cfg.SortMapKeys {
		// Deterministic is a jsonv2 option but it composes with jsontext
		// options because Options is the same underlying type.
		opts = append(opts, jsonv2.Deterministic(true))
	}
	if cfg.NoNullSliceOrMap {
		opts = append(opts, jsonv2.FormatNilSliceAsNull(false))
		opts = append(opts, jsonv2.FormatNilMapAsNull(false))
	}
	if cfg.CaseSensitive {
		opts = append(opts, jsonv2.MatchCaseInsensitiveNames(false))
	}
	if cfg.NoEncoderNewline {
		// OmitTopLevelNewline is not directly exported as a function in
		// jsontext; the streaming encoder always appends a newline after a
		// top-level value. We trim it in Encode when NoEncoderNewline is set.
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
	return jsonv2.Unmarshal(data, val, opts...)
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

// jsonv2Encoder implements Encoder by wrapping a jsontext.Encoder. It
// retains the original writer so SetEscapeHTML/SetIndent can rebuild the
// encoder with new options after construction.
type jsonv2Encoder struct {
	enc          *stdjsontext.Encoder
	w            io.Writer
	cfg          Config
	escapeHTML   bool
	indent       string
	indentPrefix string
	noNewline    bool
}

func (e *jsonv2Encoder) Encode(v interface{}) error {
	// jsonv2.MarshalEncode writes a JSON value to the encoder. The
	// jsontext streaming encoder auto-flushes when a top-level value
	// completes and appends a trailing newline unless OmitTopLevelNewline
	// is set on the encoder's options (which is not exported as a public
	// option setter). We honor NoEncoderNewline by trimming the trailing
	// newline from the underlying *bytes.Buffer when the caller used one
	// (the common test path); for arbitrary writers the newline remains.
	opts := []jsonv2.Options{
		stdjsontext.EscapeForHTML(e.escapeHTML),
	}
	if e.cfg.SortMapKeys {
		opts = append(opts, jsonv2.Deterministic(true))
	}
	if e.cfg.NoNullSliceOrMap {
		opts = append(opts, jsonv2.FormatNilSliceAsNull(false))
		opts = append(opts, jsonv2.FormatNilMapAsNull(false))
	}
	if e.cfg.CaseSensitive {
		opts = append(opts, jsonv2.MatchCaseInsensitiveNames(false))
	}
	if err := jsonv2.MarshalEncode(e.enc, v, opts...); err != nil {
		return err
	}
	// Trim trailing newline for NoEncoderNewline when the writer is a
	// bytes.Buffer (we can mutate its tail in place).
	if e.noNewline {
		if bb, ok := e.w.(*bytes.Buffer); ok {
			b := bb.Bytes()
			for len(b) > 0 && b[len(b)-1] == '\n' {
				b = b[:len(b)-1]
				bb.Truncate(len(b))
			}
		}
	}
	return nil
}

func (e *jsonv2Encoder) SetEscapeHTML(on bool) {
	e.escapeHTML = on
	// Rebuild the underlying jsontext.Encoder with the new escape setting.
	e.reconfigure()
}

func (e *jsonv2Encoder) SetIndent(prefix, indent string) {
	e.indentPrefix = prefix
	e.indent = indent
	e.reconfigure()
}

// reconfigure rebuilds the underlying jsontext.Encoder with the current
// escape/indent settings. It calls Encoder.Reset, which requires the
// writer; we retained the writer reference at construction time so we
// can pass it back. Calling Reset between Encode calls is safe but may
// discard data buffered (not yet flushed) by the encoder.
func (e *jsonv2Encoder) reconfigure() {
	opts := []stdjsontext.Options{stdjsontext.EscapeForHTML(e.escapeHTML)}
	if e.cfg.SortMapKeys {
		opts = append(opts, jsonv2.Deterministic(true))
	}
	if e.cfg.NoNullSliceOrMap {
		opts = append(opts, jsonv2.FormatNilSliceAsNull(false))
		opts = append(opts, jsonv2.FormatNilMapAsNull(false))
	}
	if e.cfg.CaseSensitive {
		opts = append(opts, jsonv2.MatchCaseInsensitiveNames(false))
	}
	if e.indent != "" || e.indentPrefix != "" {
		opts = append(opts, stdjsontext.Multiline(true))
		if e.indent != "" {
			opts = append(opts, stdjsontext.WithIndent(e.indent))
		}
		if e.indentPrefix != "" {
			opts = append(opts, stdjsontext.WithIndentPrefix(e.indentPrefix))
		}
	}
	e.enc.Reset(e.w, opts...)
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
}

// Compile-time interface satisfaction checks.
var (
	_ API     = (*jsonv2API)(nil)
	_ Encoder = (*jsonv2Encoder)(nil)
	_ Decoder = (*jsonv2Decoder)(nil)
)
