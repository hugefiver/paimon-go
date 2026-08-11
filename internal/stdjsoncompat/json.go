// Package stdjsoncompat implements the backend contract using the
// standard-library encoding/json package. It is the reflection fallback
// used by the root sonic API in this phase, and will remain available as
// the UseStdJSON engine after the fastjson backend lands.
//
// The implementation honors the subset of backend.Config options that
// encoding/json can control directly:
//   - UseNumber                 -> json.Decoder.UseNumber
//   - DisallowUnknownFields     -> json.Decoder.DisallowUnknownFields
//   - EscapeHTML                -> json.Encoder.SetEscapeHTML
//   - SortMapKeys               -> map key sorting via encoding/json
//     (encoding/json sorts map keys natively when marshalling)
//   - NoEncoderNewline          -> suppress trailing newline on streams
//
// Options that encoding/json cannot enforce (NoNullSliceOrMap,
// CompactMarshaler, NoQuoteTextMarshaler, UseInt64 for nested values,
// UseUnicodeErrors, CopyString, ValidateString, NoValidateJSONMarshaler,
// NoValidateJSONSkip, EncodeNullForInfOrNan, CaseSensitive) are accepted
// but have no effect here; later backends implement them.
package stdjsoncompat

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/bytedance/sonic/internal/backend"
	"github.com/bytedance/sonic/internal/jsonconv"
)

// Marshal serializes v under cfg. EscapeHTML is honored by post-processing
// the encoding/json output when false (encoding/json escapes HTML by
// default); SortMapKeys is honored natively by encoding/json.
func Marshal(v interface{}, cfg backend.Config) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if !cfg.EscapeHTML {
		// encoding/json always HTML-escapes when using Marshal. Undo it
		// by re-encoding through a buffer with SetEscapeHTML(false).
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err != nil {
			return nil, err
		}
		// json.Encoder.Encode appends a newline; trim it to match Marshal.
		out := buf.Bytes()
		if n := len(out); n > 0 && out[n-1] == '\n' {
			out = out[:n-1]
		}
		return out, nil
	}
	return b, nil
}

// MarshalIndent is like Marshal but applies a two-space-ish indent.
func MarshalIndent(v interface{}, prefix, indent string, cfg backend.Config) ([]byte, error) {
	b, err := json.MarshalIndent(v, prefix, indent)
	if err != nil {
		return nil, err
	}
	if !cfg.EscapeHTML {
		// encoding/json.MarshalIndent always HTML-escapes. Re-encode through
		// a buffer with escaping disabled. We can't preserve the caller's
		// prefix/indent through Encoder, so we do a manual re-indent by
		// re-marshalling then indenting the unescaped bytes.
		var raw bytes.Buffer
		enc := json.NewEncoder(&raw)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err != nil {
			return nil, err
		}
		out := raw.Bytes()
		if n := len(out); n > 0 && out[n-1] == '\n' {
			out = out[:n-1]
		}
		// Re-indent using json.Indent (no HTML escaping happens here).
		var ind bytes.Buffer
		json.Indent(&ind, out, prefix, indent)
		return ind.Bytes(), nil
	}
	return b, nil
}

// Unmarshal parses data into v under cfg.
func Unmarshal(data []byte, v interface{}, cfg backend.Config) error {
	data = escapeRawControlsInStrings(data)
	if cfg.UseNumber || cfg.UseInt64 {
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
		if cfg.UseInt64 && !cfg.UseNumber {
			jsonconv.ConvertNumbersToInt64(v)
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
		useInt64:              cfg.UseInt64,
		useNumber:             cfg.UseNumber,
		disallowUnknownFields: cfg.DisallowUnknownFields,
	}
}

// streamEncoder wraps json.Encoder and honors NoEncoderNewline.
type streamEncoder struct {
	w          io.Writer
	noNewline  bool
	escapeHTML bool
	prefix     string
	indent     string
}

func (e *streamEncoder) Encode(v interface{}) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(e.escapeHTML)
	enc.SetIndent(e.prefix, e.indent)
	if err := enc.Encode(v); err != nil {
		return err
	}
	out := buf.Bytes()
	if e.noNewline && len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	_, err := e.w.Write(out)
	return err
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
	dec                   *json.Decoder
	useInt64              bool
	useNumber             bool
	disallowUnknownFields bool
}

func (d *streamDecoder) Decode(v interface{}) error {
	if err := d.dec.Decode(v); err != nil {
		return err
	}
	if d.useInt64 && !d.useNumber {
		jsonconv.ConvertNumbersToInt64(v)
	}
	return nil
}

func (d *streamDecoder) Buffered() io.Reader {
	return d.dec.Buffered()
}

func (d *streamDecoder) DisallowUnknownFields() {
	d.disallowUnknownFields = true
	d.dec.DisallowUnknownFields()
}

func (d *streamDecoder) More() bool {
	return d.dec.More()
}

func (d *streamDecoder) UseNumber() {
	d.useNumber = true
	d.useInt64 = false
	d.dec.UseNumber()
}
