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
	"reflect"

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
// This backend validates the entire input document before resolving the path,
// including when opts.ValidateJSON is false.
func GetWithOptions(src []byte, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	if len(src) == 0 || !stdjsontext.Value(src).IsValid(stdjsontext.AllowDuplicateNames(true), stdjsontext.AllowInvalidUTF8(true)) {
		return ast.Node{}, &ast.SyntaxError{Src: string(src), Msg: "invalid JSON value"}
	}
	opts.ValidateJSON = false
	return fastjsoncompat.Get(src, opts, path...)
}

// GetStringWithOptions validates the document and searches the original
// string. The validator requires a byte copy; the search itself does not copy
// the document again, and CopyReturn copies only the selected result.
func GetStringWithOptions(src string, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	if len(src) == 0 || !stdjsontext.Value([]byte(src)).IsValid(stdjsontext.AllowDuplicateNames(true), stdjsontext.AllowInvalidUTF8(true)) {
		return ast.Node{}, &ast.SyntaxError{Src: src, Msg: "invalid JSON value"}
	}
	s := ast.NewSearcher(src)
	opts.ValidateJSON = false
	s.SearchOptions = opts
	return s.GetByPath(path...)
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
	checkNumberMode(a.cfg)
	return jsonv2.Unmarshal(data, val, a.unmarshalOpts...)
}

func checkNumberMode(cfg Config) {
	if cfg.UseNumber && cfg.UseInt64 {
		panic("can't set OptionUseInt64 and OptionUseNumber both!")
	}
}

func (a *jsonv2API) Valid(data []byte) bool {
	// Keep duplicate-name and invalid-UTF-8 handling consistent with the
	// compatibility decoding options.
	return len(data) > 0 && stdjsontext.Value(data).IsValid(stdjsontext.AllowDuplicateNames(true), stdjsontext.AllowInvalidUTF8(true))
}

func (a *jsonv2API) NewEncoder(w io.Writer) Encoder {
	return &jsonv2Encoder{
		w:           w,
		cfg:         a.cfg,
		marshalOpts: a.marshalOpts,
		noNewline:   a.cfg.NoEncoderNewline,
	}
}

func (a *jsonv2API) NewDecoder(r io.Reader) Decoder {
	checkNumberMode(a.cfg)
	// Match the syntax options of the byte-input decoder.
	dec := stdjsontext.NewDecoder(r, stdjsontext.AllowDuplicateNames(true), stdjsontext.AllowInvalidUTF8(true))
	return &jsonv2Decoder{
		dec:           dec,
		cfg:           a.cfg,
		unmarshalOpts: a.unmarshalOpts,
	}
}

// buildMarshalOptions freezes a joined set of options for reuse by Marshal.
func buildMarshalOptions(cfg Config) []jsonv2.Options {
	// Sonic follows the legacy representation of ordinary Go values:
	// omitempty, byte arrays, pointer methods, map keys, and string tags.
	// Override only the options exposed by Sonic's Config.
	return []jsonv2.Options{jsonv2.JoinOptions(
		stdjson.DefaultOptionsV1(),
		stdjsontext.EscapeForHTML(cfg.EscapeHTML),
		stdjsontext.EscapeForJS(cfg.EscapeHTML),
		jsonv2.Deterministic(cfg.SortMapKeys),
		jsonv2.FormatNilSliceAsNull(!cfg.NoNullSliceOrMap),
		jsonv2.FormatNilMapAsNull(!cfg.NoNullSliceOrMap),
	)}
}

// buildUnmarshalOptions freezes the complete options, including number hooks.
func buildUnmarshalOptions(cfg Config) []jsonv2.Options {
	opts := []jsonv2.Options{
		stdjson.DefaultOptionsV1(),
		// Retain this backend's fail-fast error handling. The legacy error
		// option would continue after custom errors and unknown fields, which
		// would invoke more user code and mutate fields after Sonic has stopped.
		stdjson.ReportErrorsWithLegacySemantics(false),
		jsonv2.RejectUnknownMembers(cfg.DisallowUnknownFields),
		jsonv2.MatchCaseInsensitiveNames(!cfg.CaseSensitive),
	}
	if cfg.UseNumber {
		opts = append(opts, jsonv2.WithUnmarshalers(numberUnmarshalers))
	} else if cfg.UseInt64 {
		opts = append(opts, jsonv2.WithUnmarshalers(int64Unmarshalers))
	} else {
		opts = append(opts, jsonv2.WithUnmarshalers(interfacePointerGuard))
	}
	return []jsonv2.Options{jsonv2.JoinOptions(opts...)}
}

// These hooks only handle numbers decoded into an empty interface. Concrete
// non-nil pointers retain legacy decoding, including custom UnmarshalJSON.
// ReadValue has already validated the token; no secondary decoder or walk of
// the destination is needed, so untouched fields and custom values stay intact.
var (
	numberUnmarshalers    = makeNumberUnmarshalers(false)
	int64Unmarshalers     = makeNumberUnmarshalers(true)
	interfacePointerGuard = jsonv2.UnmarshalFromFunc(func(_ *stdjsontext.Decoder, out *any) error {
		if err := breakInterfaceSelfPointer(out); err != nil {
			return err
		}
		return errors.ErrUnsupported
	})
)

// A non-nil pointer inside an interface normally preserves its concrete
// destination. The special value "var x any; x = &x" must instead allow x to
// be replaced. Otherwise jsonv2 follows the pointer forever. Only inspect the
// interface currently being decoded; untouched fields are never traversed.
func breakInterfaceSelfPointer(out *any) error {
	v := reflect.ValueOf(*out)
	var seen [8]reflect.Value
	visited := seen[:0]
	for v.IsValid() && v.Kind() == reflect.Pointer && !v.IsNil() {
		// User-defined decoding methods own their pointer traversal.
		if v.Type().Implements(reflect.TypeFor[json.Unmarshaler]()) {
			return nil
		}
		e := v.Elem()
		if e.Kind() == reflect.Interface && !e.IsNil() && e.Elem().Type() == v.Type() && e.Elem().Pointer() == v.Pointer() {
			e.SetZero()
			return nil
		}
		for _, previous := range visited {
			if previous.Type() == v.Type() && previous.Pointer() == v.Pointer() {
				return errCyclicInterfacePointer
			}
		}
		visited = append(visited, v)
		for e.Kind() == reflect.Interface && !e.IsNil() {
			e = e.Elem()
		}
		v = e
	}
	return nil
}

var errCyclicInterfacePointer = errors.New("json: cyclic pointer while decoding an interface")

func makeNumberUnmarshalers(useInt64 bool) *jsonv2.Unmarshalers {
	return jsonv2.UnmarshalFromFunc(func(dec *stdjsontext.Decoder, out *any) error {
		if err := breakInterfaceSelfPointer(out); err != nil {
			return err
		}
		if dec.PeekKind() != stdjsontext.Kind('0') {
			return errors.ErrUnsupported
		}
		if v := reflect.ValueOf(*out); v.IsValid() && v.Kind() == reflect.Pointer && !v.IsNil() {
			return errors.ErrUnsupported
		}
		raw, err := dec.ReadValue()
		if err != nil {
			return err
		}
		if useInt64 {
			value, err := jsonconv.ParseNumber(string(raw))
			if err != nil {
				return err
			}
			*out = value
		} else {
			// The decoder owns raw; converting to string preserves this number
			// across later reads, including when its buffer is reused.
			*out = json.Number(string(raw))
		}
		return nil
	})
}

// jsonv2Encoder implements Encoder by buffering each value and writing
// it out with short-write retry. It does not wrap a jsontext.Encoder
// directly because jsontext.Flush silently drops short writes, which
// would violate the documented io.ErrShortWrite retry contract.
type jsonv2Encoder struct {
	w            io.Writer
	cfg          Config
	marshalOpts  []jsonv2.Options
	indent       string
	indentPrefix string
	noNewline    bool
	buf          bytes.Buffer
	indentBuf    bytes.Buffer
	enc          *stdjsontext.Encoder
}

func (e *jsonv2Encoder) Encode(v interface{}) error {
	// Buffer the complete value before writing so a marshaling error cannot
	// publish partial JSON. Reset also recovers the encoder after such errors.
	// Drop unusually large output buffers instead of retaining them forever.
	const maxRetainedBuffer = 1 << 20
	if e.buf.Cap() > maxRetainedBuffer {
		e.buf = bytes.Buffer{}
	}
	e.buf.Reset()
	if e.enc == nil {
		e.enc = stdjsontext.NewEncoder(&e.buf, e.marshalOpts...)
	} else {
		e.enc.Reset(&e.buf, e.marshalOpts...)
	}
	if err := jsonv2.MarshalEncode(e.enc, v, e.marshalOpts...); err != nil {
		return err
	}
	b := e.buf.Bytes()
	// jsontext terminates every top-level value with a newline. Handle it
	// here so indentation and NoEncoderNewline follow the same path.
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	if e.indent != "" || e.indentPrefix != "" {
		// json.Indent accepts arbitrary prefix/indent characters; the
		// jsontext WithIndent options panic on non-space characters.
		if e.indentBuf.Cap() > maxRetainedBuffer {
			e.indentBuf = bytes.Buffer{}
		}
		e.indentBuf.Reset()
		if err := stdjson.Indent(&e.indentBuf, b, e.indentPrefix, e.indent); err != nil {
			return err
		}
		b = e.indentBuf.Bytes()
	}
	if !e.noNewline {
		b = append(b, '\n')
	}
	return writeAll(e.w, b)
}

func (e *jsonv2Encoder) SetEscapeHTML(on bool) {
	e.cfg.EscapeHTML = on
	e.marshalOpts = buildMarshalOptions(e.cfg)
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
	dec           *stdjsontext.Decoder
	cfg           Config
	unmarshalOpts []jsonv2.Options
	err           error
	targetType    reflect.Type
	retainsRaw    bool
}

func (d *jsonv2Decoder) Decode(v interface{}) error {
	if d.err != nil {
		return d.err
	}
	if target := reflect.TypeOf(v); target != d.targetType {
		d.targetType = target
		d.retainsRaw = jsonconv.RetainsRawInput(target)
	}
	if d.retainsRaw {
		// NoCopyRawMessage keeps the bytes passed to UnmarshalJSON. A
		// streaming jsontext decoder reuses its input buffer on later reads,
		// so destinations that can retain raw input need an owning value.
		// Ordinary typed destinations still use the direct streaming path.
		var raw stdjsontext.Value
		raw, d.err = d.dec.ReadValue()
		if d.err == nil {
			d.err = jsonv2.Unmarshal(bytes.Clone(raw), v, d.unmarshalOpts...)
		}
		return d.err
	}
	d.err = jsonv2.UnmarshalDecode(d.dec, v, d.unmarshalOpts...)
	return d.err
}

func (d *jsonv2Decoder) Buffered() io.Reader {
	if d.err != nil {
		return bytes.NewReader(nil)
	}
	// Return a copy of the unread buffer so the reader remains valid
	// after subsequent Decoder calls.
	if b := d.dec.UnreadBuffer(); len(b) > 0 {
		for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
			b = b[1:]
		}
		return bytes.NewReader(append([]byte(nil), b...))
	}
	return bytes.NewReader(nil)
}

func (d *jsonv2Decoder) DisallowUnknownFields() {
	d.cfg.DisallowUnknownFields = true
	d.unmarshalOpts = buildUnmarshalOptions(d.cfg)
}

func (d *jsonv2Decoder) More() bool {
	if d.err != nil {
		return false
	}
	// Closing delimiters and EOF do not start another value.
	kind := d.dec.PeekKind()
	return kind != stdjsontext.KindInvalid && kind != '}' && kind != ']'
}

func (d *jsonv2Decoder) UseNumber() {
	d.cfg.UseNumber = true
	d.cfg.UseInt64 = false
	d.unmarshalOpts = buildUnmarshalOptions(d.cfg)
}

// Compile-time interface satisfaction checks.
var (
	_ API     = (*jsonv2API)(nil)
	_ Encoder = (*jsonv2Encoder)(nil)
	_ Decoder = (*jsonv2Decoder)(nil)
)
