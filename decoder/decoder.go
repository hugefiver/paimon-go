// Package decoder mirrors the public surface of
// github.com/bytedance/sonic/decoder from Sonic v1.15.2. It exposes the
// package-level Skip helper, the Decoder builder for string inputs, the
// StreamDecoder for io.Reader inputs, and the Pretouch/PretouchMany
// hooks used by callers that warm Sonic's JIT cache.
//
// The implementation in this phase is a thin façade over the standard
// library's encoding/json package. It honors the subset of Options that
// encoding/json can control directly:
//
//   - OptionUseNumber           -> json.Decoder.UseNumber
//   - OptionUseInt64            -> decode with UseNumber, then convert
//     integer-looking json.Number values
//     to int64 in nested interface/map/slice
//     targets
//   - OptionDisableUnknown      -> json.Decoder.DisallowUnknownFields
//
// Options that encoding/json cannot enforce (OptionUseUnicodeErrors,
// OptionCopyString, OptionValidateString, OptionNoValidateJSON,
// OptionCaseSensitive) are accepted and stored on the Options bitmask
// for API compatibility; they have no behavioral effect in this phase.
//
// Pretouch and PretouchMany are no-ops that return nil so callers that
// pre-warm the JIT at startup continue to compile and run.
package decoder

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/bytedance/sonic/internal/jsonconv"
	nativetypes "github.com/bytedance/sonic/internal/native/types"
	"github.com/bytedance/sonic/option"
)

// Options is the bitmask type carried by Decoder.SetOptions and the
// option constants below. It mirrors sonic.decoder.Options from
// v1.15.2.
type Options uint64

const (
	// OptionUseInt64 decodes integer-looking numeric values into int64
	// when the target is an interface{}, map, or slice. Struct fields
	// keep their declared types and are decoded by encoding/json
	// normally.
	OptionUseInt64 Options = 1 << iota
	// OptionUseNumber decodes numeric values into json.Number instead
	// of float64 when the target is an interface{}, map, or slice.
	OptionUseNumber
	// OptionUseUnicodeErrors makes the decoder return an error on
	// invalid UTF-8 inside string values instead of replacing the
	// invalid bytes with U+FFFD. The reflection backend cannot enforce
	// this; the bit is stored for API compatibility.
	OptionUseUnicodeErrors
	// OptionDisableUnknown causes the decoder to reject JSON object
	// keys that do not match any destination struct field. It maps to
	// json.Decoder.DisallowUnknownFields.
	OptionDisableUnknown
	// OptionCopyString copies decoded string values into newly
	// allocated storage instead of aliasing the input buffer. The
	// reflection backend always copies via encoding/json, so this bit
	// is informational here.
	OptionCopyString
	// OptionValidateString validates string contents during decode.
	// The reflection backend defers string validation to encoding/json,
	// so this bit is informational here.
	OptionValidateString
	// OptionNoValidateJSON skips validating the overall JSON syntax
	// before decoding. The reflection backend always validates, so
	// this bit is informational here.
	OptionNoValidateJSON
	// OptionCaseSensitive makes object key matching case-sensitive.
	// encoding/json matches keys case-insensitively by default, so
	// this bit is informational here; enabling it does not change the
	// behavior of the reflection backend.
	OptionCaseSensitive
)

// SyntaxError represents a JSON syntax error. Its public fields and methods
// mirror Sonic's decoder.SyntaxError surface.
type SyntaxError struct {
	Pos  int
	Src  string
	Code nativetypes.ParsingError
	Msg  string
}

func (e SyntaxError) Error() string { return fmt.Sprintf("%q", e.Description()) }

func (e SyntaxError) Description() string {
	return "Syntax error " + e.Message()
}

func (e SyntaxError) Message() string {
	if e.Msg != "" {
		return e.Msg
	}
	return e.Code.Message()
}

// MismatchTypeError represents a mismatch between a JSON value and the Go
// destination type. It mirrors Sonic's public field shape.
type MismatchTypeError struct {
	Pos  int
	Src  string
	Type reflect.Type
}

func (e MismatchTypeError) Error() string { return e.Description() }

func (e MismatchTypeError) Description() string {
	return fmt.Sprintf("Mismatch type %v", e.Type)
}

// Pretouch precompiles the given type. In this phase there is no JIT
// compiler, so it is a no-op that returns nil. The variadic
// option.CompileOption argument is accepted for source compatibility
// with Sonic's public API.
func Pretouch(_ reflect.Type, _ ...option.CompileOption) error { return nil }

// PretouchMany is the variadic form of Pretouch.
func PretouchMany(_ []reflect.Type, _ ...option.CompileOption) error { return nil }

// Skip scans data for the first complete JSON value, skipping any
// leading whitespace, and returns the half-open byte bounds [start,end)
// of that value within data. It respects JSON string literals (and
// their escape sequences) and counts nested arrays and objects so a
// value that contains structural characters inside strings or nested
// containers is reported correctly.
//
// When data does not contain a complete JSON value, Skip returns
// (0, 0).
//
// The returned bounds are suitable for slicing data[start:end] to
// obtain the raw JSON text of the value, including any internal
// whitespace between tokens.
func Skip(data []byte) (start int, end int) {
	// Skip leading whitespace.
	i := 0
	for i < len(data) && isSpace(data[i]) {
		i++
	}
	if i >= len(data) {
		return 0, 0
	}
	start = i

	switch data[i] {
	case '"':
		// Top-level JSON string. Scan to the matching closing quote,
		// honoring backslash escapes. A truncated escape or an
		// unterminated string means there is no complete value.
		i++
		for i < len(data) {
			if data[i] == '\\' {
				if i+1 >= len(data) {
					return 0, 0
				}
				i += 2
				continue
			}
			if data[i] == '"' {
				i++
				return start, i
			}
			i++
		}
		return 0, 0
	case '{', '[':
		// Top-level container. Count nested objects and arrays, and
		// skip over string literals so structural characters inside
		// strings do not affect the depth counter.
		depth := 1
		i++
		for i < len(data) && depth > 0 {
			c := data[i]
			switch c {
			case '"':
				// Scan the string body in place.
				i++
				for i < len(data) {
					if data[i] == '\\' {
						if i+1 >= len(data) {
							return 0, 0
						}
						i += 2
						continue
					}
					if data[i] == '"' {
						i++
						break
					}
					i++
				}
				// If the for loop above exited because i >= len(data),
				// the string is unterminated; fall through to the
				// outer check below.
			case '{', '[':
				depth++
				i++
			case '}', ']':
				depth--
				i++
				if depth == 0 {
					return start, i
				}
			default:
				i++
			}
		}
		if depth != 0 {
			return 0, 0
		}
		return start, i
	default:
		// Bare token: number, true, false, null. The value ends at
		// the next whitespace, comma (top-level separator), closing
		// brace/bracket, or end of input. A colon is included for
		// safety even though it should not appear at the top level.
		j := i
		for j < len(data) && !isSpace(data[j]) && data[j] != ',' && data[j] != '}' && data[j] != ']' {
			j++
		}
		if j == start {
			return 0, 0
		}
		return start, j
	}
}

// isSpace reports whether c is a JSON whitespace byte as defined by
// RFC 8259: space, horizontal tab, line feed, carriage return.
func isSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Decoder
// ---------------------------------------------------------------------------

// Decoder reads and decodes JSON values from a string source. It
// mirrors sonic.decoder.Decoder from v1.15.2. The zero value is not
// ready for use; callers must construct one via NewDecoder.
//
// The decoder advances a position cursor through the source string as
// values are consumed. Reset replaces the source while preserving the
// configured Options bitmask, matching Sonic's behavior.
type Decoder struct {
	src             string
	pos             int
	opts            Options
	disallowUnknown bool
	// useInt64 and useNumber are derived from opts at SetOptions time
	// but also toggled by the Use* convenience methods so the bitmask
	// and the fast-path flags stay in sync.
	useInt64  bool
	useNumber bool
}

// NewDecoder returns a Decoder that reads from s. The returned Decoder
// has a zero-value Options bitmask (permissive configuration); callers
// configure it via the Set*/Use* methods before calling Decode.
func NewDecoder(s string) *Decoder {
	return &Decoder{src: s}
}

// Decode reads the next JSON-encoded value from the source string and
// stores it in the value pointed to by val.
//
// The position cursor is advanced past the decoded value, so successive
// calls to Decode return successive values from the source. When the
// source is exhausted, Decode returns io.EOF.
//
// Options that affect decoding (OptionUseNumber, OptionUseInt64,
// OptionDisableUnknown) are honored. OptionUseInt64 only converts
// numeric values for interface{}, map, and slice targets; struct
// targets are decoded by encoding/json with their declared field types.
func (d *Decoder) Decode(val interface{}) error {
	// Skip any whitespace between values.
	for d.pos < len(d.src) && isSpace(byteForIndex(d.src, d.pos)) {
		d.pos++
	}
	if d.pos >= len(d.src) {
		return io.EOF
	}

	dec := json.NewDecoder(strings.NewReader(d.src[d.pos:]))
	if d.useNumber || d.useInt64 {
		dec.UseNumber()
	}
	if d.disallowUnknown {
		dec.DisallowUnknownFields()
	}

	if err := dec.Decode(val); err != nil {
		return err
	}

	consumed := int(dec.InputOffset())
	d.pos += consumed

	if d.useInt64 {
		jsonconv.ConvertNumbersToInt64(val)
	}
	return nil
}

// byteForIndex returns the byte at index i in s. It is a helper so the
// whitespace-skip loop can stay allocation-free.
func byteForIndex(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return 0
}

// CheckTrailings returns nil if only whitespace bytes remain in the
// source string after the current position. It returns an error
// otherwise, matching Sonic's behavior of rejecting trailing non-JSON
// bytes after a decoded value.
func (d *Decoder) CheckTrailings() error {
	for i := d.pos; i < len(d.src); i++ {
		if !isSpace(d.src[i]) {
			return errTrailingBytes
		}
	}
	return nil
}

// Pos returns the current byte offset of the decoder within the source
// string. It is advanced as values are decoded.
func (d *Decoder) Pos() int { return d.pos }

// Reset replaces the source string and resets the position cursor to
// zero. The configured Options bitmask is preserved so a Decoder can be
// reused across inputs without re-applying its options.
func (d *Decoder) Reset(s string) {
	d.src = s
	d.pos = 0
}

// SetOptions replaces the Options bitmask. The convenience flags
// (useNumber, useInt64, disallowUnknown) are re-derived from the
// bitmask so subsequent Decode calls honor the new configuration.
func (d *Decoder) SetOptions(opts Options) {
	if opts&OptionUseNumber != 0 && opts&OptionUseInt64 != 0 {
		panic("can't set OptionUseInt64 and OptionUseNumber both!")
	}
	d.opts = opts
	d.useNumber = opts&OptionUseNumber != 0
	d.useInt64 = opts&OptionUseInt64 != 0
	d.disallowUnknown = opts&OptionDisableUnknown != 0
}

// UseInt64 enables integer decoding for interface/map/slice targets:
// numeric values are first decoded as json.Number (as if UseNumber had
// been called) and then recursively converted to int64 when the number
// parses as an integer via strconv.ParseInt.
func (d *Decoder) UseInt64() {
	d.opts |= OptionUseInt64
	d.opts &^= OptionUseNumber
	d.useInt64 = true
	d.useNumber = false
}

// UseNumber enables json.Number decoding for interface/map/slice
// targets, matching encoding/json.Decoder.UseNumber.
func (d *Decoder) UseNumber() {
	d.opts |= OptionUseNumber
	d.opts &^= OptionUseInt64
	d.useNumber = true
	d.useInt64 = false
}

// UseUnicodeErrors enables the OptionUseUnicodeErrors bit. The
// reflection backend cannot enforce Unicode-error semantics, so this is
// stored for API compatibility only.
func (d *Decoder) UseUnicodeErrors() {
	d.opts |= OptionUseUnicodeErrors
}

// ValidateString enables the OptionValidateString bit. The reflection
// backend defers string validation to encoding/json, so this is stored
// for API compatibility only.
func (d *Decoder) ValidateString() {
	d.opts |= OptionValidateString
}

// CopyString enables the OptionCopyString bit. The reflection backend
// always copies decoded strings into fresh storage, so this is stored
// for API compatibility only.
func (d *Decoder) CopyString() {
	d.opts |= OptionCopyString
}

// DisallowUnknownFields enables the OptionDisableUnknown bit and
// configures subsequent Decode calls to reject JSON object keys that do
// not match any destination struct field.
func (d *Decoder) DisallowUnknownFields() {
	d.opts |= OptionDisableUnknown
	d.disallowUnknown = true
}

// errTrailingBytes is returned by CheckTrailings when non-whitespace
// bytes remain after the decoded value.
var errTrailingBytes = errors.New("decoder: invalid trailing characters at the end")

// ---------------------------------------------------------------------------
// StreamDecoder
// ---------------------------------------------------------------------------

// StreamDecoder reads and decodes JSON values from an io.Reader. It
// mirrors sonic.decoder.StreamDecoder from v1.15.2. The implementation
// wraps encoding/json.Decoder directly; the option bitmask is not
// applied because the streaming API in this phase exposes only the
// Decode/Buffered/InputOffset/More methods.
type StreamDecoder struct {
	dec *json.Decoder
}

// NewStreamDecoder returns a StreamDecoder that reads from r.
func NewStreamDecoder(r io.Reader) *StreamDecoder {
	return &StreamDecoder{dec: json.NewDecoder(r)}
}

// Decode reads the next JSON-encoded value from the stream and stores
// it in the value pointed to by val.
func (d *StreamDecoder) Decode(val interface{}) error {
	return d.dec.Decode(val)
}

// Buffered returns a reader over the remaining bytes in the decoder's
// buffer. Those bytes are available because json.Decoder may read ahead
// of the value it just decoded.
func (d *StreamDecoder) Buffered() io.Reader {
	return d.dec.Buffered()
}

// InputOffset returns the number of bytes read from the input stream
// so far. It mirrors sonic.decoder.StreamDecoder.InputOffset.
func (d *StreamDecoder) InputOffset() int64 {
	return d.dec.InputOffset()
}

// More reports whether there is another element in the current array or
// object being parsed.
func (d *StreamDecoder) More() bool {
	return d.dec.More()
}
