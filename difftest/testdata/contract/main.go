// A single consumer is compiled against both implementations. In addition to
// checking behavior, this catches missing public methods at compile time.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/decoder"
	"github.com/bytedance/sonic/encoder"
	"github.com/bytedance/sonic/unquote"
)

type record map[string]any

var results = record{}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func encoded(key string, cfg sonic.Config, value any) {
	b, err := cfg.Froze().Marshal(value)
	results[key] = record{"ok": err == nil, "json": string(b)}
}
func number(key, src string, cfg sonic.Config) {
	var v any
	err := cfg.Froze().Unmarshal([]byte(src), &v)
	if err != nil {
		results[key] = record{"ok": false}
		return
	}
	results[key] = record{"ok": true, "value": fmt.Sprintf("%T:%v", v, v)}
}

type counted struct {
	Calls int
	Raw   string
}

func (v *counted) UnmarshalJSON(data []byte) error { v.Calls++; v.Raw = string(data); return nil }

type ignored struct {
	Self  *ignored `json:"-"`
	Keep  any      `json:"-"`
	Value any      `json:"value"`
}
type fields struct {
	UserID string `json:"user_id"`
	N      int    `json:"n"`
	S      string `json:"s"`
	B      bool   `json:"b"`
}
type empty struct {
	N     int            `json:"n,omitempty"`
	B     bool           `json:"b,omitempty"`
	A     [2]byte        `json:"a"`
	Slice []int          `json:"slice"`
	Map   map[string]int `json:"map"`
}

func main() {
	if sonic.APIKind != sonic.UseSonicJSON {
		panic("contract must run against native Sonic, not its fallback")
	}
	for _, src := range []string{"0", "-0", "1", "1.5", "1e3", "9223372036854775807", "9223372036854775808", "-9223372036854775809", "1e400", `{"a":[1,1.5,1e3],"b":true}`} {
		number("int64/"+src, src, sonic.Config{UseInt64: true})
		number("number/"+src, src, sonic.Config{UseNumber: true})
	}
	func() {
		defer func() { results["conflict_froze_panics"] = recover() != nil }()
		_ = sonic.Config{UseInt64: true, UseNumber: true}.Froze()
	}()
	func() {
		defer func() { results["conflict_decode_panics"] = recover() != nil }()
		var v any
		_ = sonic.Config{UseNumber: true, UseInt64: true}.Froze().Unmarshal([]byte(`1`), &v)
	}()
	func() {
		defer func() { results["conflict_stream_panics"] = recover() != nil }()
		_ = sonic.Config{UseNumber: true, UseInt64: true}.Froze().NewDecoder(strings.NewReader("1"))
	}()
	for _, s := range []string{"\x00", "\a", "\v", "\x1f", "<>&", "hello\n", "quote\"slash\\"} {
		q := encoder.Quote(s)
		results["quote/"+s] = record{"value": q, "valid": json.Valid([]byte(q))}
	}
	for _, s := range []string{`\uD800`, `\uD800\u0041`, `\uDC00`, `\ud83d\ude00`, `a\nb`} {
		v, err := unquote.String(s)
		results["unquote/"+s] = record{"ok": err == 0, "value": v}
	}
	encoded("ordinary/default", sonic.Config{}, empty{})
	encoded("ordinary/std", sonic.Config{EscapeHTML: true, SortMapKeys: true}, map[string]any{"z": "<>&", "a": empty{}})
	encoded("raw/duplicate", sonic.Config{}, json.RawMessage(`{"a":1,"a":2}`))
	encoded("float/infinity", sonic.Config{}, math.Inf(1))
	f := fields{UserID: "original", N: 9, S: "keep", B: true}
	must(sonic.Unmarshal([]byte(`{"n":null,"s":null,"b":null,"user-id":"other"}`), &f))
	results["fields/null_case"] = f
	for _, mode := range []struct {
		name string
		cfg  sonic.Config
	}{{"default", sonic.Config{}}, {"int64", sonic.Config{UseInt64: true}}, {"number", sonic.Config{UseNumber: true}}} {
		v := ignored{Keep: json.Number("9")}
		v.Self = &v
		must(mode.cfg.Froze().Unmarshal([]byte(`{"value":1}`), &v))
		results["state/"+mode.name] = record{"cycle": v.Self == &v, "keep": fmt.Sprintf("%T:%v", v.Keep, v.Keep), "value": fmt.Sprintf("%T:%v", v.Value, v.Value)}
		c := &counted{}
		var dst any = c
		must(mode.cfg.Froze().Unmarshal([]byte(`{"a":1}`), &dst))
		results["custom/"+mode.name] = c
		var dup map[string]any
		must(mode.cfg.Froze().Unmarshal([]byte(`{"x":{"a":1},"x":{"b":2}}`), &dup))
		results["duplicate/"+mode.name] = fmt.Sprintf("%v", dup)
	}
	d := decoder.NewStreamDecoder(strings.NewReader("1 2"))
	d.UseNumber()
	d.DisallowUnknownFields()
	var v any
	must(d.Decode(&v))
	results["stream/number"] = fmt.Sprintf("%T:%v", v, v)
	d.SetOptions(decoder.OptionUseInt64)
	must(d.Decode(&v))
	results["stream/int64"] = fmt.Sprintf("%T:%v", v, v)
	rootDec := sonic.ConfigDefault.NewDecoder(strings.NewReader("1 \n\t2"))
	var first int
	must(rootDec.Decode(&first))
	remaining, err := io.ReadAll(rootDec.Buffered())
	must(err)
	results["stream/buffered"] = string(remaining)
	var out bytes.Buffer
	e := sonic.Config{NoEncoderNewline: true}.Froze().NewEncoder(&out)
	must(e.Encode("<tag>"))
	e.SetEscapeHTML(true)
	must(e.Encode("<tag>"))
	results["stream/encode"] = out.String()
	var b = []byte("prefix:")
	must(encoder.EncodeInto(&b, 42, 0))
	results["encode_into"] = string(b)
	a := ast.NewArray([]ast.Node{ast.NewNumber("1")})
	p := a.Index(0)
	must(a.Add(ast.NewNumber("2")))
	*p = ast.NewNumber("3")
	ab, err := a.MarshalJSON()
	must(err)
	results["ast/add_pointer"] = string(ab)
	o := ast.NewObject([]ast.Pair{ast.NewPair("a", ast.NewNumber("1")), ast.NewPair("b", ast.NewNumber("2")), ast.NewPair("c", ast.NewNumber("3"))})
	p = o.Get("b")
	_, err = o.Unset("a")
	must(err)
	num, err := p.Number()
	must(err)
	results["ast/unset_pointer"] = string(num)
	a = ast.NewArray([]ast.Node{{}, ast.NewNumber("1")})
	ab, err = a.MarshalJSON()
	must(err)
	results["ast/none"] = string(ab)
	var indices []int
	must(a.ForEach(func(s ast.Sequence, _ *ast.Node) bool { indices = append(indices, s.Index); return true }))
	results["ast/none_indices"] = indices
	a = ast.NewNumber("1")
	_, err = a.Set("key", ast.NewNull())
	results["ast/unsupported"] = errors.Is(err, ast.ErrUnsupportType)
	n := ast.NewRaw(`{"a":[ 1, 2 ]}`)
	must(n.Load())
	raw, err := n.Get("a").Raw()
	must(err)
	results["ast/lazy"] = record{"raw": raw, "is_raw": n.Get("a").IsRaw()}
	must(json.NewEncoder(os.Stdout).Encode(results))
}
