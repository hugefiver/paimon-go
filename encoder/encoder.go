// Package encoder mirrors the public surface of
// github.com/bytedance/sonic/encoder from Sonic v1.15.2. It exposes the
// package-level Encode helpers, the Encoder builder, and the streaming
// StreamEncoder used by callers that want a json.Encoder-like API with
// Sonic's option set.
//
// The implementation in this phase is a thin façade over
// internal/stdjsoncompat (encoding/json) and internal/backend.Config. It
// honors the subset of Options that the reflection backend can control
// directly: EscapeHTML, SortMapKeys (including the CompatibleWithStd
// alias), NoEncoderNewline on streams, and EncodeNullForInfOrNan mapping
// through to backend.Config. Options the reflection backend cannot
// enforce (CompactMarshaler, NoQuoteTextMarshaler, NoNullSliceOrMap,
// ValidateString, NoValidateJSONMarshaler) are mapped into Config so
// later fastjson-based backends pick them up without API changes.
//
// Pretouch and PretouchMany are no-ops in this phase; they return nil so
// callers that warm the JIT at startup continue to compile and run.
package encoder

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strconv"

	"github.com/bytedance/sonic/internal/backend"
	"github.com/bytedance/sonic/internal/stdjsoncompat"
	"github.com/bytedance/sonic/option"
)

// Options is the bitmask type carried by the encoder package's Encode
// helpers and Encoder.Opts. It mirrors sonic.encoder.Options from
// v1.15.2.
type Options uint64

const (
	bitSortMapKeys = iota
	bitEscapeHTML
	bitCompactMarshaler
	bitNoQuoteTextMarshaler
	bitNoNullSliceOrMap
	bitValidateString
	bitNoValidateJSONMarshaler
	bitNoEncoderNewline
	bitEncodeNullForInfOrNan
)

const (
	// SortMapKeys causes map keys to be emitted in sorted order.
	SortMapKeys Options = 1 << bitSortMapKeys
	// EscapeHTML escapes <, >, &, U+2028 and U+2029 inside string values
	// to their \u00XX forms so the output is safe to embed in HTML.
	EscapeHTML Options = 1 << bitEscapeHTML
	// CompactMarshaler requests compact (no whitespace) output from
	// json.Marshaler implementations. The reflection backend always
	// produces compact output, so this is informational here.
	CompactMarshaler Options = 1 << bitCompactMarshaler
	// NoQuoteTextMarshaler emits the raw bytes returned by
	// encoding.TextMarshaler without wrapping them in a JSON string.
	NoQuoteTextMarshaler Options = 1 << bitNoQuoteTextMarshaler
	// NoNullSliceOrMap emits empty slices and maps as [] and {} instead
	// of null when the value is a nil slice/map.
	NoNullSliceOrMap Options = 1 << bitNoNullSliceOrMap
	// ValidateString validates string values during encode.
	ValidateString Options = 1 << bitValidateString
	// NoValidateJSONMarshaler skips validating the output of
	// json.Marshaler implementations.
	NoValidateJSONMarshaler Options = 1 << bitNoValidateJSONMarshaler
	// NoEncoderNewline suppresses the trailing newline written by
	// StreamEncoder.Encode.
	NoEncoderNewline Options = 1 << bitNoEncoderNewline
	// CompatibleWithStd aligns behavior with encoding/json: map keys
	// are sorted and HTML is escaped. It is the encoder-side analogue
	// of sonic.ConfigStd.
	CompatibleWithStd Options = SortMapKeys | EscapeHTML | CompactMarshaler
	// EncodeNullForInfOrNan emits null for non-finite floats instead
	// of returning an error.
	EncodeNullForInfOrNan Options = 1 << bitEncodeNullForInfOrNan
)

// EnableFallback is kept for source compatibility with Sonic v1.15.2.
// Sonic uses it to toggle the fallback-to-stdlib path; this replacement
// is always backed by encoding/json so fallback is never enabled.
const EnableFallback = false

// optionToConfig translates each concrete encoder option bit independently.
// CompatibleWithStd is the composite alias SortMapKeys | EscapeHTML |
// CompactMarshaler, so checking the three concrete bits also gives the alias
// its documented behavior without treating any single member as the alias.
func optionToConfig(opts Options) backend.Config {
	return backend.Config{
		EscapeHTML:              opts&EscapeHTML != 0,
		SortMapKeys:             opts&SortMapKeys != 0,
		CompactMarshaler:        opts&CompactMarshaler != 0,
		NoQuoteTextMarshaler:    opts&NoQuoteTextMarshaler != 0,
		NoNullSliceOrMap:        opts&NoNullSliceOrMap != 0,
		ValidateString:          opts&ValidateString != 0,
		NoValidateJSONMarshaler: opts&NoValidateJSONMarshaler != 0,
		NoEncoderNewline:        opts&NoEncoderNewline != 0,
		EncodeNullForInfOrNan:   opts&EncodeNullForInfOrNan != 0,
	}
}

// Encode marshals v into a compact JSON byte slice under opts. It is the
// package-level entry point matching sonic.encoder.Encode.
func Encode(val interface{}, opts Options) ([]byte, error) {
	return stdjsoncompat.Marshal(val, optionToConfig(opts))
}

// EncodeIndented marshals v with the given prefix and indent applied to
// each level of nesting. It matches sonic.encoder.EncodeIndented.
func EncodeIndented(val interface{}, prefix string, indent string, opts Options) ([]byte, error) {
	return stdjsoncompat.MarshalIndent(val, prefix, indent, optionToConfig(opts))
}

// EncodeInto appends the JSON encoding of val to *buf. The existing
// contents of *buf are preserved; the encoded value is appended after
// them. A nil *buf is treated as an error to make accidental misuse
// explicit.
//
// If encoding fails, *buf is left untouched.
func EncodeInto(buf *[]byte, val interface{}, opts Options) error {
	if buf == nil {
		return errNilBuffer
	}
	out, err := Encode(val, opts)
	if err != nil {
		return err
	}
	*buf = append(*buf, out...)
	return nil
}

// HTMLEscape appends to dst the JSON-escaped form of src, replacing <,
// >, & and the U+2028 / U+2029 paragraph separators inside string
// literals with their \u00XX escape sequences. It matches
// sonic.encoder.HTMLEscape and encoding/json.HTMLEscape.
func HTMLEscape(dst []byte, src []byte) []byte {
	var b bytes.Buffer
	json.HTMLEscape(&b, src)
	return append(dst, b.Bytes()...)
}

// Quote returns a double-quoted JSON string literal form of s. It is a
// thin wrapper over strconv.Quote; callers that need HTML-escaped
// output should compose HTMLEscape on top.
func Quote(s string) string {
	return strconv.Quote(s)
}

// Valid reports whether data is a single well-formed JSON value. It
// also returns the offset of the first non-whitespace byte so callers
// can locate the start of the JSON value. When the data is invalid but
// contains only whitespace (or is empty), start is len(data).
func Valid(data []byte) (ok bool, start int) {
	start = firstNonSpaceOffset(data)
	if !json.Valid(data) {
		return false, start
	}
	return true, start
}

// firstNonSpaceOffset returns the index of the first byte in data that
// is not a JSON whitespace character (space, tab, newline, carriage
// return). It returns len(data) when no such byte exists.
func firstNonSpaceOffset(data []byte) int {
	for i, b := range data {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return i
		}
	}
	return len(data)
}

// Pretouch precompiles a type for the encoder. In this phase there is
// no JIT compiler, so it is a no-op that returns nil. The variadic
// option.CompileOption argument is accepted for source compatibility
// with Sonic's public API.
func Pretouch(_ reflect.Type, _ ...option.CompileOption) error {
	return nil
}

// PretouchMany is the variadic form of Pretouch.
func PretouchMany(_ []reflect.Type, _ ...option.CompileOption) error {
	return nil
}

// Encoder is the builder-style encoder exposed by the encoder package.
// Callers configure it via the Set* methods (and SortKeys), then call
// Encode to produce a JSON byte slice. The zero value is a valid
// permissive encoder that produces compact JSON with no HTML escaping
// and no sorting.
type Encoder struct {
	// Opts is the Options bitmask applied to Encode. The Set* methods
	// mutate this field in place.
	Opts Options
	// prefix and indent hold the indentation settings. When indent is
	// empty Encode produces compact output.
	prefix string
	indent string
}

// Encode marshals v under the encoder's current configuration. When
// indent is non-empty the output is indented; otherwise it is compact.
func (e *Encoder) Encode(v interface{}) ([]byte, error) {
	if e.indent == "" {
		return Encode(v, e.Opts)
	}
	return EncodeIndented(v, e.prefix, e.indent, e.Opts)
}

// SetCompactMarshaler toggles the CompactMarshaler option bit.
func (e *Encoder) SetCompactMarshaler(on bool) {
	if on {
		e.Opts |= CompactMarshaler
	} else {
		e.Opts &^= CompactMarshaler
	}
}

// SetEscapeHTML toggles the EscapeHTML option bit.
func (e *Encoder) SetEscapeHTML(on bool) {
	if on {
		e.Opts |= EscapeHTML
	} else {
		e.Opts &^= EscapeHTML
	}
}

// SetIndent sets the prefix and indent strings used when producing
// indented output. An empty indent string restores compact output.
func (e *Encoder) SetIndent(prefix, indent string) {
	e.prefix = prefix
	e.indent = indent
}

// SetNoEncoderNewline toggles the NoEncoderNewline option bit. This is
// honored by StreamEncoder.Encode; Encoder.Encode itself never emits a
// trailing newline.
func (e *Encoder) SetNoEncoderNewline(on bool) {
	if on {
		e.Opts |= NoEncoderNewline
	} else {
		e.Opts &^= NoEncoderNewline
	}
}

// SetNoQuoteTextMarshaler toggles the NoQuoteTextMarshaler option bit.
func (e *Encoder) SetNoQuoteTextMarshaler(on bool) {
	if on {
		e.Opts |= NoQuoteTextMarshaler
	} else {
		e.Opts &^= NoQuoteTextMarshaler
	}
}

// SetNoValidateJSONMarshaler toggles the NoValidateJSONMarshaler option
// bit.
func (e *Encoder) SetNoValidateJSONMarshaler(on bool) {
	if on {
		e.Opts |= NoValidateJSONMarshaler
	} else {
		e.Opts &^= NoValidateJSONMarshaler
	}
}

// SetValidateString toggles the ValidateString option bit.
func (e *Encoder) SetValidateString(on bool) {
	if on {
		e.Opts |= ValidateString
	} else {
		e.Opts &^= ValidateString
	}
}

// SortKeys enables SortMapKeys. It is the builder-style accessor used
// by callers that prefer method chaining.
func (e *Encoder) SortKeys() *Encoder {
	e.Opts |= SortMapKeys
	return e
}

// StreamEncoder is the streaming encoder analogue of encoding/json's
// json.Encoder. It wraps an io.Writer and an Encoder; each Encode call
// produces the JSON encoding of one value, writes it to the writer,
// and appends a trailing newline unless NoEncoderNewline is set.
type StreamEncoder struct {
	Encoder
	// w is the destination writer. It is set at construction time and
	// is not replaced.
	w io.Writer
}

// NewStreamEncoder constructs a StreamEncoder writing to w. The
// returned value embeds an Encoder whose Opts is the zero value
// (permissive configuration); callers configure it via the Set*
// methods before calling Encode.
func NewStreamEncoder(w io.Writer) *StreamEncoder {
	return &StreamEncoder{
		Encoder: Encoder{},
		w:       w,
	}
}

// Encode marshals val under the encoder's current configuration and
// writes the result to the underlying writer. A trailing newline is
// appended unless NoEncoderNewline is set on the embedded Encoder's
// Opts, matching the default behavior of encoding/json's
// json.Encoder.Encode and Sonic's encoder.StreamEncoder.
func (e *StreamEncoder) Encode(val interface{}) error {
	out, err := e.Encoder.Encode(val)
	if err != nil {
		return err
	}
	for offset := 0; offset < len(out); {
		n, err := e.w.Write(out[offset:])
		if err != nil {
			return err
		}
		if n <= 0 || n > len(out)-offset {
			return io.ErrShortWrite
		}
		offset += n
	}
	if e.Opts&NoEncoderNewline == 0 {
		for offset := 0; offset < len(newlineBytes); {
			n, err := e.w.Write(newlineBytes[offset:])
			if err != nil {
				return err
			}
			if n <= 0 || n > len(newlineBytes)-offset {
				return io.ErrShortWrite
			}
			offset += n
		}
	}
	return nil
}

// newlineBytes is the single-byte newline appended by StreamEncoder
// when NoEncoderNewline is not set.
var newlineBytes = []byte{'\n'}

// errNilBuffer is returned by EncodeInto when called with a nil *[]byte.
type stringError struct{ msg string }

func (e *stringError) Error() string { return e.msg }

// errNilBuffer is returned by EncodeInto when buf is nil.
var errNilBuffer = &stringError{msg: "encoder: EncodeInto called with nil buffer"}
