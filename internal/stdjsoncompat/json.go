// Package stdjsoncompat implements the backend contract using the
// standard-library encoding/json package. It is the reflection fallback
// used by the root sonic API in this phase, and will remain available as
// the UseStdJSON engine after the fastjson backend lands.
//
// The implementation honors the subset of backend.Config options that
// encoding/json can control directly:
//   - UseNumber                 -> json.Decoder.UseNumber
//   - UseInt64                  -> UseNumber plus recursive conversion of
//     interface-contained integers to int64
//   - DisallowUnknownFields     -> json.Decoder.DisallowUnknownFields
//   - EscapeHTML                -> json.Encoder.SetEscapeHTML
//   - SortMapKeys               -> map key sorting via encoding/json
//     (encoding/json sorts map keys natively when marshalling)
//   - NoEncoderNewline          -> suppress trailing newline on streams
//
// Options that encoding/json cannot enforce (NoNullSliceOrMap,
// CompactMarshaler, NoQuoteTextMarshaler, UseUnicodeErrors, CopyString,
// ValidateString, NoValidateJSONMarshaler, NoValidateJSONSkip,
// EncodeNullForInfOrNan, CaseSensitive) are accepted but have no effect here;
// later backends implement them.
package stdjsoncompat

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync"

	"github.com/bytedance/sonic/option"

	"github.com/bytedance/sonic/internal/backend"
	"github.com/bytedance/sonic/internal/jsonconv"
)

// Marshal serializes v under cfg. SortMapKeys is honored natively by
// encoding/json.
type encodeState struct {
	buf         []byte
	enc         *json.Encoder
	dropNewline bool
}

func (s *encodeState) Write(p []byte) (int, error) {
	n := len(p)
	if s.dropNewline && n > 0 && p[n-1] == '\n' {
		p = p[:n-1]
	}
	s.buf = append(s.buf, p...)
	return n, nil
}

func newEncodeState() *encodeState {
	s := new(encodeState)
	s.enc = json.NewEncoder(s)
	return s
}

var encodePool = sync.Pool{New: func() any { return newEncodeState() }}

func (s *encodeState) reset() {
	if uint(cap(s.buf)) > option.LimitBufferSize {
		s.buf = nil
		s.enc = json.NewEncoder(s) // also release the encoder's indentation buffer
	} else {
		s.buf = s.buf[:0]
	}
}

func (s *encodeState) encode(v any, cfg backend.Config, prefix, indent string) error {
	s.enc.SetEscapeHTML(cfg.EscapeHTML)
	s.enc.SetIndent(prefix, indent)
	return s.enc.Encode(v)
}

func Marshal(v interface{}, cfg backend.Config) ([]byte, error) {
	if cfg.EscapeHTML {
		return json.Marshal(v)
	}
	return marshal(v, cfg, "", "")
}

func marshal(v any, cfg backend.Config, prefix, indent string) ([]byte, error) {
	s := encodePool.Get().(*encodeState)
	defer func() { s.dropNewline = false; s.reset(); encodePool.Put(s) }()
	s.dropNewline = true
	if err := s.encode(v, cfg, prefix, indent); err != nil {
		return nil, err
	}
	return bytes.Clone(s.buf), nil
}

// Append marshals directly into dst, preserving its contents on error. The
// standard encoder completes validation before invoking its writer.
func Append(dst *[]byte, v any, cfg backend.Config) error {
	s := encodePool.Get().(*encodeState)
	retained := s.buf
	s.buf = *dst
	s.dropNewline = true
	err := s.encode(v, cfg, "", "")
	if err == nil {
		*dst = s.buf
	}
	s.buf = retained
	s.dropNewline = false
	encodePool.Put(s)
	return err
}

// MarshalIndent is like Marshal but applies the caller's prefix and indent.
func MarshalIndent(v interface{}, prefix, indent string, cfg backend.Config) ([]byte, error) {
	if cfg.EscapeHTML {
		return json.MarshalIndent(v, prefix, indent)
	}
	return marshal(v, cfg, prefix, indent)
}

// Unmarshal parses data into v under cfg.
func Unmarshal(data []byte, v interface{}, cfg backend.Config) error {
	data = normalizeUnmarshalInput(data)
	if cfg.UseInt64 && !cfg.UseNumber {
		return jsonconv.UnmarshalInt64(data, v, cfg.DisallowUnknownFields)
	}
	if cfg.UseNumber {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if cfg.DisallowUnknownFields {
			dec.DisallowUnknownFields()
		}
		if err := dec.Decode(v); err != nil {
			return err
		}
		if err := rejectTrailingData(dec); err != nil {
			return err
		}
		return nil
	}
	if cfg.DisallowUnknownFields {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(v); err != nil {
			return err
		}
		return rejectTrailingData(dec)
	}
	// Only plain dynamic targets use the token cursor. It retains full syntax
	// validation and falls back for existing maps or concrete interface pointers.
	switch v.(type) {
	case *any, *map[string]any:
		return jsonconv.UnmarshalDynamic(data, v)
	}

	return json.Unmarshal(data, v)
}

var errTrailingData = errors.New("invalid trailing data after top-level value")

func rejectTrailingData(dec *json.Decoder) error {
	var extra struct{}
	err := dec.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return errTrailingData
	}
	return err
}

func escapeRawControlsInStrings(data []byte) []byte {
	if !containsControlByte(data) {
		return data
	}
	var out []byte
	inString := false
	escaped := false
	for i, b := range data {
		if out != nil {
			if inString && !escaped && b < 0x20 {
				out = append(out, '\\', 'u', '0', '0', hexDigit(b>>4), hexDigit(b&0x0f))
			} else {
				out = append(out, b)
			}
		}

		if escaped {
			escaped = false
			continue
		}
		if b == '\\' && inString {
			escaped = true
			continue
		}
		if b == '"' {
			inString = !inString
			continue
		}
		if inString && b < 0x20 && out == nil {
			out = make([]byte, 0, len(data)+6)
			out = append(out, data[:i]...)
			out = append(out, '\\', 'u', '0', '0', hexDigit(b>>4), hexDigit(b&0x0f))
		}
	}
	if out == nil {
		return data
	}
	return out
}

// A compact JSON document normally contains no literal C0 bytes. Check eight
// bytes at once before running the quote/escape state machine. Borrow between
// bytes can cause a harmless false positive, but never hide a control byte.
func containsControlByte(data []byte) bool {
	for len(data) >= 8 {
		word := binary.LittleEndian.Uint64(data)
		if (word-0x2020202020202020)&^word&0x8080808080808080 != 0 {
			return true
		}
		data = data[8:]
	}
	for _, b := range data {
		if b < 0x20 {
			return true
		}
	}
	return false
}

func hexDigit(b byte) byte {
	if b < 10 {
		return '0' + b
	}
	return 'a' + (b - 10)
}

// Valid reports whether data is a single well-formed JSON value.
func Valid(data []byte) bool {
	return json.Valid(data)
}

// NewEncoder returns a streaming encoder writing to w under cfg.
func NewEncoder(w io.Writer, cfg backend.Config) backend.StreamEncoder {
	return &streamEncoder{
		w:          w,
		state:      newEncodeState(),
		noNewline:  cfg.NoEncoderNewline,
		escapeHTML: cfg.EscapeHTML,
	}
}

// NewDecoder returns a streaming decoder reading from r under cfg.
func NewDecoder(r io.Reader, cfg backend.Config) backend.StreamDecoder {
	dec := json.NewDecoder(r)
	if cfg.UseNumber || cfg.UseInt64 {
		dec.UseNumber()
	}
	if cfg.DisallowUnknownFields {
		dec.DisallowUnknownFields()
	}
	return &streamDecoder{
		dec:                   dec,
		reader:                r,
		useInt64:              cfg.UseInt64,
		useNumber:             cfg.UseNumber,
		disallowUnknownFields: cfg.DisallowUnknownFields,
	}
}

// streamEncoder wraps json.Encoder and honors NoEncoderNewline.
type streamEncoder struct {
	state      *encodeState
	w          io.Writer
	noNewline  bool
	escapeHTML bool
	prefix     string
	indent     string
}

func (e *streamEncoder) Encode(v interface{}) error {
	s := e.state
	defer s.reset()
	s.dropNewline = e.noNewline
	if err := s.encode(v, backend.Config{EscapeHTML: e.escapeHTML}, e.prefix, e.indent); err != nil {
		return err
	}
	out := s.buf
	for len(out) > 0 {
		n, err := e.w.Write(out)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(out) {
			return io.ErrShortWrite
		}
		out = out[n:]
	}
	return nil
}

func (e *streamEncoder) SetEscapeHTML(on bool) {
	e.escapeHTML = on
}

func (e *streamEncoder) SetIndent(prefix, indent string) {
	e.prefix = prefix
	e.indent = indent
}

// streamDecoder wraps json.Decoder and forwards the streaming knobs.
type streamDecoder struct {
	reader                io.Reader
	detach                bool
	targetType            reflect.Type
	retainsRaw            bool
	terminal              error
	dec                   *json.Decoder
	useInt64              bool
	useNumber             bool
	disallowUnknownFields bool
}

func (d *streamDecoder) Decode(v interface{}) error {
	if d.terminal != nil {
		return d.terminal
	}
	if d.detach {
		d.reader = io.MultiReader(d.dec.Buffered(), d.reader)
		d.dec = json.NewDecoder(d.reader)
		if d.useNumber {
			d.dec.UseNumber()
		}
		if d.disallowUnknownFields {
			d.dec.DisallowUnknownFields()
		}
		d.detach = false
	}
	if target := reflect.TypeOf(v); target != d.targetType {
		d.targetType = target
		d.retainsRaw = jsonconv.RetainsRawInput(target)
	}
	var err error
	if d.useInt64 && !d.useNumber {
		var raw json.RawMessage
		if err = d.dec.Decode(&raw); err == nil {
			err = jsonconv.UnmarshalInt64(raw, v, d.disallowUnknownFields)
		}
	} else {
		err = d.dec.Decode(v)
	}
	d.detach = d.retainsRaw && (!d.useInt64 || d.useNumber)
	if err != nil {
		d.terminal = err
	}
	return err
}

func (d *streamDecoder) Buffered() io.Reader {
	if d.terminal != nil {
		return bytes.NewReader(nil)
	}
	reader := d.dec.Buffered()
	// Native Sonic consumes buffered whitespace after the decoded value,
	// without reading ahead from the underlying stream.
	if b, ok := reader.(*bytes.Reader); ok {
		for {
			c, err := b.ReadByte()
			if err != nil {
				break
			}
			if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
				_ = b.UnreadByte()
				break
			}
		}
	}
	return reader
}

func (d *streamDecoder) DisallowUnknownFields() {
	d.disallowUnknownFields = true
	d.dec.DisallowUnknownFields()
}

func (d *streamDecoder) More() bool {
	if d.terminal != nil {
		return false
	}
	return d.dec.More()
}

func (d *streamDecoder) UseNumber() {
	d.useNumber = true
	d.useInt64 = false
	d.dec.UseNumber()
}
