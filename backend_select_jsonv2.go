//go:build sonic_jsonv2 && !sonic_stdjson && goexperiment.jsonv2

package sonic

import (
	"io"

	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/internal/backend"
	"github.com/bytedance/sonic/stdjsonv2"
)

type jsonv2Backend struct {
	api stdjsonv2.API
}

var (
	_ backend.Backend       = jsonv2Backend{}
	_ backend.StreamEncoder = stdjsonv2.Encoder(nil)
	_ backend.StreamDecoder = stdjsonv2.Decoder(nil)
)

func newBackend(cfg backend.Config) backend.Backend {
	return jsonv2Backend{api: toStdJSONV2Config(cfg).Froze()}
}

func toStdJSONV2Config(cfg backend.Config) stdjsonv2.Config {
	return stdjsonv2.Config{
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

func (b jsonv2Backend) Marshal(v interface{}, _ backend.Config) ([]byte, error) {
	return b.api.Marshal(v)
}

func (b jsonv2Backend) MarshalIndent(v interface{}, prefix, indent string, _ backend.Config) ([]byte, error) {
	return b.api.MarshalIndent(v, prefix, indent)
}

func (b jsonv2Backend) Unmarshal(data []byte, v interface{}, _ backend.Config) error {
	return b.api.Unmarshal(data, v)
}

func (b jsonv2Backend) Valid(data []byte) bool {
	return b.api.Valid(data)
}

func (b jsonv2Backend) Get(data []byte, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	return stdjsonv2.GetWithOptions(data, opts, path...)
}

func (b jsonv2Backend) NewEncoder(w io.Writer, _ backend.Config) backend.StreamEncoder {
	return b.api.NewEncoder(w)
}

func (b jsonv2Backend) NewDecoder(r io.Reader, _ backend.Config) backend.StreamDecoder {
	return b.api.NewDecoder(r)
}

func selectedGet(data []byte, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	return stdjsonv2.GetWithOptions(data, opts, path...)
}
