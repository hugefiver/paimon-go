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
	"os"
	"reflect"

	"github.com/bytedance/sonic/decoder"
	"github.com/bytedance/sonic/internal/backend"
	nativetypes "github.com/bytedance/sonic/internal/native/types"
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
// them. A nil *buf panics.
//
// If encoding fails, *buf is left untouched.
func EncodeInto(buf *[]byte, val interface{}, opts Options) error {
	if buf == nil {
		panic("user-supplied buffer buf is nil")
	}
	return stdjsoncompat.Append(buf, val, optionToConfig(opts))
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
// UTF-8 bytes are retained, and control characters use JSON escapes.
func Quote(s string) string {
	return quoteJSON(s)
}

// Valid checks Sonic's structural JSON grammar and returns the first value's
// start on success, or the offending cursor on failure. Like native Sonic,
// string escape contents and UTF-8 are not validated by this entry point.
func Valid(data []byte) (ok bool, start int) {
	if len(data) == 0 {
		return false, -1
	}
	start, end := decoder.Skip(data)
	if start < 0 {
		if start == -int(nativetypes.ERR_RECURSE_EXCEED_MAX) {
			return false, end - 1
		}
		return false, validationCursor(data, end-1)
	}
	for i := end; i < len(data); i++ {
		switch data[i] {
		case ' ', '\t', '\n', '\r':
		default:
			// Skip can stop before an invalid continuation of a number;
			// ValidateOne consumes the entire numeric run first.
			if cursor := validationCursor(data, i); cursor >= 0 {
				return false, cursor
			}
			return false, i
		}
	}
	return true, start
}

// validationCursor replays the token boundaries of native ValidateOne only
// when Skip's cursor is not sufficient. Structural/EOF errors retain Skip's
// diagnostic cursor; literal and numeric errors use ValidateOne's cursor.
func validationCursor(data []byte, fallback int) int {
	s := validationScanner{data: data, fallback: fallback, cursor: -1}
	if !s.value(0) {
		return s.cursor
	}
	s.space()
	if s.pos < len(data) {
		return s.pos
	}
	return -1
}

type validationScanner struct {
	data     []byte
	pos      int
	fallback int
	cursor   int
}

// Match native Sonic's startup SIMD selection for the supported mode overrides.
var validationAVX2 = os.Getenv("SONIC_MODE") != "noavx" && os.Getenv("SONIC_MODE") != "noavx2"

func (s *validationScanner) space() {
	for s.pos < len(s.data) {
		switch s.data[s.pos] {
		case ' ', '\t', '\n', '\r':
			s.pos++
		default:
			return
		}
	}
}

func (s *validationScanner) value(depth int) bool {
	s.space()
	if s.pos >= len(s.data) || depth > 4096 {
		s.cursor = s.fallback
		return false
	}
	switch s.data[s.pos] {
	case '[':
		s.pos++
		s.space()
		if s.pos < len(s.data) && s.data[s.pos] == ']' {
			s.pos++
			return true
		}
		for {
			if !s.value(depth + 1) {
				return false
			}
			s.space()
			if s.pos >= len(s.data) || s.data[s.pos] == ']' {
				break
			}
			if s.data[s.pos] != ',' {
				break
			}
			s.pos++
		}
		if s.pos < len(s.data) && s.data[s.pos] == ']' {
			s.pos++
			return true
		}
	case '{':
		s.pos++
		s.space()
		if s.pos < len(s.data) && s.data[s.pos] == '}' {
			s.pos++
			return true
		}
		for s.pos < len(s.data) && s.data[s.pos] == '"' {
			if !s.string() {
				break
			}
			s.space()
			if s.pos >= len(s.data) || s.data[s.pos] != ':' {
				s.cursor = s.fallback
				return false
			}
			s.pos++
			if !s.value(depth + 1) {
				return false
			}
			s.space()
			if s.pos >= len(s.data) || s.data[s.pos] != ',' {
				break
			}
			s.pos++
			s.space()
			if s.pos >= len(s.data) || s.data[s.pos] != '"' {
				s.cursor = s.fallback
				return false
			}
		}
		if s.pos < len(s.data) && s.data[s.pos] == '}' {
			s.pos++
			return true
		}
	case '"':
		if s.string() {
			return true
		}
	case 't', 'f', 'n':
		return s.literal()
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return s.number()
	}
	s.cursor = s.fallback
	return false
}

func (s *validationScanner) string() bool {
	s.pos++
	for s.pos < len(s.data) {
		switch s.data[s.pos] {
		case '\\':
			s.pos += 2
		case '"':
			s.pos++
			return true
		default:
			s.pos++
		}
	}
	return false
}

func (s *validationScanner) literal() bool {
	start := s.pos
	literal := "true"
	switch s.data[start] {
	case 'f':
		literal = "false"
	case 'n':
		literal = "null"
	}
	// The native four-byte literal comparison checks the remaining *whole*
	// input, not just the token. Its 'f' comparison has a size_t underflow
	// for inputs shorter than four bytes, so those take the mismatch path.
	if len(s.data) >= len(literal)-1 && start+len(literal) > len(s.data) {
		s.cursor = len(s.data) - 1
		return false
	}
	for i := 0; i < len(literal); i++ {
		at := start + i
		if at >= len(s.data) || s.data[at] != literal[i] {
			s.cursor = at - 1
			return false
		}
	}
	s.pos += len(literal)
	return true
}

func (s *validationScanner) number() bool {
	start := s.pos
	negative := s.data[s.pos] == '-'
	if negative {
		s.pos++
		if s.pos >= len(s.data) || s.data[s.pos] < '0' || s.data[s.pos] > '9' {
			s.cursor = s.fallback
			return false
		}
	}
	base := s.pos
	if s.data[base] == '0' && (base+1 == len(s.data) || (s.data[base+1] != '.' && s.data[base+1] != 'e' && s.data[base+1] != 'E')) {
		s.pos++
		return true
	}
	dot, exponent, sign := -1, -1, -1
	// Native ValidateOne scans complete 32-byte AVX2 blocks, then 16-byte
	// SSE blocks, before its scalar tail. Within each block it checks
	// repeated dots, then exponents, then signs (rather than the earliest
	// duplicate of any kind).
	for len(s.data)-s.pos >= 16 {
		blockSize := 16
		if validationAVX2 && len(s.data)-s.pos >= 32 {
			blockSize = 32
		}
		block := s.pos
		first := [3]int{-1, -1, -1}
		second := [3]int{-1, -1, -1}
		n := 0
		for n < blockSize {
			c := s.data[block+n]
			kind := -1
			switch {
			case c >= '0' && c <= '9':
			case c == '.':
				kind = 0
			case c == 'e' || c == 'E':
				kind = 1
			case c == '+' || c == '-':
				kind = 2
			default:
				goto checkBlock
			}
			if kind >= 0 {
				if first[kind] < 0 {
					first[kind] = block + n - base
				} else if second[kind] < 0 {
					second[kind] = block + n - base
				}
			}
			n++
		}
	checkBlock:
		for kind := range second {
			if second[kind] >= 0 {
				s.numberError(start, negative, second[kind]+1)
				return false
			}
		}
		for kind, previous := range [...]int{dot, exponent, sign} {
			if previous >= 0 && first[kind] >= 0 {
				s.numberError(start, negative, first[kind]+1)
				return false
			}
		}
		if first[0] >= 0 {
			dot = first[0]
		}
		if first[1] >= 0 {
			exponent = first[1]
		}
		if first[2] >= 0 {
			sign = first[2]
		}
		s.pos += n
		if n != blockSize {
			goto checkNumber
		}
	}
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		switch {
		case c >= '0' && c <= '9':
		case c == '.':
			if dot >= 0 {
				s.numberError(start, negative, s.pos-base+1)
				return false
			}
			dot = s.pos - base
		case c == 'e' || c == 'E':
			if exponent >= 0 {
				s.numberError(start, negative, s.pos-base+1)
				return false
			}
			exponent = s.pos - base
		case c == '+' || c == '-':
			if sign >= 0 {
				s.numberError(start, negative, s.pos-base+1)
				return false
			}
			sign = s.pos - base
		default:
			goto checkNumber
		}
		s.pos++
	}
checkNumber:
	n := s.pos - base
	bad := 0
	switch {
	case dot == 0 || exponent == 0 || sign == 0:
		bad = 1
	case dot == n-1 || exponent == n-1 || sign == n-1:
		bad = n
	case sign >= 0 && exponent != sign-1:
		bad = sign + 1
	case dot >= 0 && exponent >= 0 && dot > exponent-1:
		bad = dot + 1
	case dot >= 0 && exponent >= 0 && dot == exponent-1:
		bad = exponent + 1
	}
	if bad != 0 {
		s.numberError(start, negative, bad)
		return false
	}
	return true
}

func (s *validationScanner) numberError(start int, negative bool, consumed int) {
	s.cursor = start + consumed - 2
	if negative {
		s.cursor++
	}
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
	// prefix and indent hold the indentation settings. When both are
	// empty Encode produces compact output.
	prefix string
	indent string
}

// Encode marshals v under the encoder's current configuration. When prefix
// or indent is non-empty the output is indented. Encoder.Encode never adds a
// trailing newline; StreamEncoder controls record separators separately.
func (e *Encoder) Encode(v interface{}) ([]byte, error) {
	if e.prefix == "" && e.indent == "" {
		return Encode(v, e.Opts)
	}
	out, err := EncodeIndented(v, e.prefix, e.indent, e.Opts)
	if err != nil {
		return nil, err
	}
	return out, nil
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
// indented output. Empty prefix and indent strings restore compact output.
func (e *Encoder) SetIndent(prefix, indent string) {
	e.prefix = prefix
	e.indent = indent
}

// SetNoEncoderNewline toggles the StreamEncoder record separator. It has no
// effect on Encoder.Encode, which never appends a newline.
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
	w        io.Writer
	stream   backend.StreamEncoder
	lastOpts Options
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
	if e.stream == nil || e.lastOpts != e.Opts {
		e.stream = stdjsoncompat.NewEncoder(e.w, optionToConfig(e.Opts))
		e.lastOpts = e.Opts
	}
	e.stream.SetEscapeHTML(e.Opts&EscapeHTML != 0)
	e.stream.SetIndent(e.prefix, e.indent)
	return e.stream.Encode(val)
}
