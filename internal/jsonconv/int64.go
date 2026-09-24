package jsonconv

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

var (
	unmarshalerType     = reflect.TypeFor[json.Unmarshaler]()
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// UnmarshalInt64 decodes a JSON value using Sonic's interface-number rules.
// Conversion happens while consuming tokens. Omitted fields, preexisting map
// entries, and values returned by custom unmarshalers are never traversed.
// The standard library handles custom methods and typed scalar values.
func UnmarshalInt64(data []byte, dst any, disallowUnknown bool) error {
	if !json.Valid(data) {
		return json.Unmarshal(data, dst)
	}
	v := reflect.ValueOf(dst)
	if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() {
		return &json.InvalidUnmarshalError{Type: reflect.TypeOf(dst)}
	}
	d := numberDecoder{data: data, disallow: disallowUnknown}
	d.space()
	err := d.value(v)
	if custom, ok := err.(customError); ok {
		return custom.err
	}
	return err
}

type customError struct{ err error }

func (e customError) Error() string { return e.err.Error() }
func customResult(err error) error {
	if err != nil {
		return customError{err}
	}
	return nil
}
func recoverable(err error) bool { _, ok := err.(*json.UnmarshalTypeError); return ok }

type numberDecoder struct {
	data       []byte
	pos        int
	disallow   bool
	useFloat64 bool
}

func (d *numberDecoder) value(v reflect.Value) error {
	// Custom methods own their result. Never revisit a value they construct.
	if v.Kind() == reflect.Pointer {
		if d.data[d.pos] == 'n' && v.CanSet() {
			d.pos += 4
			v.SetZero()
			return nil
		}
		if v.IsNil() {
			if !v.CanSet() {
				d.raw()
				return &json.InvalidUnmarshalError{Type: v.Type()}
			}
			v.Set(reflect.New(v.Type().Elem()))
		}
		if v.Type().Implements(unmarshalerType) || v.Type().Implements(textUnmarshalerType) {
			return customResult(json.Unmarshal(d.raw(), v.Interface()))
		}
		return d.value(v.Elem())
	}
	if v.CanAddr() && (v.Addr().Type().Implements(unmarshalerType) || v.Addr().Type().Implements(textUnmarshalerType)) {
		return customResult(json.Unmarshal(d.raw(), v.Addr().Interface()))
	}
	if v.Kind() == reflect.Interface {
		if !v.IsNil() && v.Elem().Kind() == reflect.Pointer && !v.Elem().IsNil() &&
			!(v.CanAddr() && v.Elem().Pointer() == v.Addr().Pointer()) {
			if d.data[d.pos] == 'n' {
				d.pos += 4
				v.SetZero()
				return nil
			}
			return d.value(v.Elem())
		}
		if v.NumMethod() != 0 {
			return json.Unmarshal(d.raw(), v.Addr().Interface())
		}
		value, err := d.anyValue()
		if err != nil {
			return err
		}
		if value == nil {
			v.SetZero()
		} else {
			v.Set(reflect.ValueOf(value))
		}
		return nil
	}
	if d.data[d.pos] == 'n' {
		d.pos += 4
		if v.Kind() == reflect.Map || v.Kind() == reflect.Slice {
			v.SetZero()
		}
		return nil
	}
	switch {
	case d.data[d.pos] == '{' && v.Kind() == reflect.Struct:
		fields := cachedFields(v.Type())
		d.pos++
		d.space()
		var first error
		for d.data[d.pos] != '}' {
			key := d.key()
			field, ok := fields.exact[key]
			if !ok {
				field, ok = fields.folded[foldName(key)]
			}
			if !ok {
				if d.disallow {
					return fmt.Errorf("json: unknown field %q", key)
				}
				d.raw()
			} else {
				fv, err := fieldByIndex(v, field.index)
				if err != nil {
					d.raw()
				} else if field.quoted && d.data[d.pos] != 'n' {
					raw := d.raw()
					if raw[0] != '"' {
						err = fmt.Errorf("json: invalid use of ,string struct tag, trying to unmarshal unquoted value into %v", fv.Type())
					} else {
						var quoted string
						err = json.Unmarshal(raw, &quoted)
						if err == nil {
							if custom, handled := decodeQuotedCustom(fv, []byte(quoted)); handled {
								err = custom
							} else if !json.Valid([]byte(quoted)) {
								err = fmt.Errorf("json: invalid use of ,string struct tag, trying to unmarshal %q into %v", quoted, fv.Type())
							} else {
								inner := numberDecoder{data: []byte(quoted), disallow: d.disallow}
								inner.space()
								err = inner.value(fv)
							}
						}
					}
				} else {
					err = d.value(fv)
				}
				if err != nil {
					if !recoverable(err) {
						return err
					}
					if first == nil {
						first = err
					}
				}
			}
			d.separator()
		}
		d.pos++
		return first
	case d.data[d.pos] == '{' && v.Kind() == reflect.Map:
		kt := v.Type().Key()
		if !mapKeySupported(kt) {
			return json.Unmarshal(d.raw(), v.Addr().Interface())
		}
		if v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
		}
		d.pos++
		d.space()
		var first error
		for d.data[d.pos] != '}' {
			key := d.key()
			k, err := decodeMapKey(key, kt)
			if err != nil {
				d.raw()
			} else {
				elem := reflect.New(v.Type().Elem()).Elem()
				err = d.value(elem)
				v.SetMapIndex(k, elem)
			}
			if err != nil {
				if !recoverable(err) {
					return err
				}
				if first == nil {
					first = err
				}
			}
			d.separator()
		}
		d.pos++
		return first
	case d.data[d.pos] == '[' && (v.Kind() == reflect.Slice || v.Kind() == reflect.Array):
		d.pos++
		d.space()
		i := 0
		var first error
		for d.data[d.pos] != ']' {
			if v.Kind() == reflect.Slice {
				if i >= v.Cap() {
					v.Grow(1)
				}
				if i >= v.Len() {
					v.SetLen(i + 1)
				}
			}
			if i < v.Len() {
				if err := d.value(v.Index(i)); err != nil {
					if !recoverable(err) {
						if v.Kind() == reflect.Slice {
							v.SetLen(i + 1)
						}
						return err
					}
					if first == nil {
						first = err
					}
				}
			} else {
				d.raw()
			}
			i++
			d.separator()
		}
		d.pos++
		if v.Kind() == reflect.Slice {
			if i == 0 {
				v.Set(reflect.MakeSlice(v.Type(), 0, 0))
			} else {
				v.SetLen(i)
			}
		} else {
			for j := i; j < v.Len(); j++ {
				v.Index(j).SetZero()
			}
		}
		return first
	default:
		return json.Unmarshal(d.raw(), v.Addr().Interface())
	}
}

func (d *numberDecoder) anyValue() (any, error) {
	switch d.data[d.pos] {
	case 'n':
		d.pos += 4
		return nil, nil
	case 't':
		d.pos += 4
		return true, nil
	case 'f':
		d.pos += 5
		return false, nil
	case '"':
		return d.string(), nil
	case '[':
		out := make([]any, 0)
		d.pos++
		d.space()
		var first error
		for d.data[d.pos] != ']' {
			v, err := d.anyValue()
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			d.separator()
		}
		d.pos++
		return out, first
	case '{':
		out := make(map[string]any)
		d.pos++
		d.space()
		var first error
		for d.data[d.pos] != '}' {
			key := d.key()
			v, err := d.anyValue()
			if err != nil {
				return nil, err
			}
			out[key] = v
			d.separator()
		}
		d.pos++
		return out, first
	default:
		if d.useFloat64 {
			f, err := strconv.ParseFloat(string(d.raw()), 64)
			if err != nil {
				return nil, err
			}
			return f, nil
		}
		return ParseNumberBytes(d.raw())
	}
}

// This cursor consumes validated JSON once, without scanning each nested
// container's whole subtree or allocating RawMessages for individual members.
func (d *numberDecoder) space() {
	for d.pos < len(d.data) {
		switch d.data[d.pos] {
		case ' ', '\n', '\r', '\t':
			d.pos++
		default:
			return
		}
	}
}
func (d *numberDecoder) separator() {
	d.space()
	if d.data[d.pos] == ',' {
		d.pos++
		d.space()
	}
}
func (d *numberDecoder) key() string { key := d.string(); d.space(); d.pos++; d.space(); return key }
func (d *numberDecoder) string() string {
	raw := d.raw()
	if !bytes.ContainsRune(raw, '\\') && utf8.Valid(raw) {
		return string(raw[1 : len(raw)-1])
	}
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}
func (d *numberDecoder) raw() []byte {
	start := d.pos
	d.pos = valueEnd(d.data, d.pos)
	return d.data[start:d.pos]
}
func valueEnd(raw []byte, i int) int {
	switch raw[i] {
	case '"':
		for i++; i < len(raw); i++ {
			if raw[i] == '\\' {
				i++
			} else if raw[i] == '"' {
				return i + 1
			}
		}
	case '[', '{':
		depth := 1
		for i++; i < len(raw); i++ {
			switch raw[i] {
			case '"':
				i = valueEnd(raw, i) - 1
			case '[', '{':
				depth++
			case ']', '}':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
		}
	default:
		for i < len(raw) && raw[i] != ',' && raw[i] != ']' && raw[i] != '}' && raw[i] != ' ' && raw[i] != '\n' && raw[i] != '\r' && raw[i] != '\t' {
			i++
		}
		return i
	}
	return len(raw)
}

func mapKeySupported(t reflect.Type) bool {
	if reflect.PointerTo(t).Implements(textUnmarshalerType) {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	}
	return false
}
func decodeMapKey(s string, t reflect.Type) (reflect.Value, error) {
	v := reflect.New(t).Elem()
	if v.Addr().Type().Implements(textUnmarshalerType) {
		err := v.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(s))
		return v, customResult(err)
	}
	switch t.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, t.Bits())
		if err != nil {
			return v, &json.UnmarshalTypeError{Value: "number " + s, Type: t}
		}
		v.SetInt(n)
	default:
		n, err := strconv.ParseUint(s, 10, t.Bits())
		if err != nil {
			return v, &json.UnmarshalTypeError{Value: "number " + s, Type: t}
		}
		v.SetUint(n)
	}
	return v, nil
}

type jsonField struct {
	name           string
	index          []int
	tagged, quoted bool
}
type structFields struct{ exact, folded map[string]jsonField }

var fieldsCache sync.Map

func cachedFields(t reflect.Type) structFields {
	if f, ok := fieldsCache.Load(t); ok {
		return f.(structFields)
	}
	var all []jsonField
	var walk func(reflect.Type, []int, map[reflect.Type]bool)
	walk = func(t reflect.Type, path []int, seen map[reflect.Type]bool) {
		if seen[t] {
			return
		}
		seen[t] = true
		defer delete(seen, t)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if f.PkgPath != "" && (!f.Anonymous || ft.Kind() != reflect.Struct) {
				continue
			}
			name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			if !validTagName(name) {
				name = ""
			}
			index := append(append([]int(nil), path...), i)
			if f.Anonymous && name == "" && ft.Kind() == reflect.Struct {
				walk(ft, index, seen)
				continue
			}
			tagged := name != ""
			if name == "" {
				name = f.Name
			}
			quoted := false
			switch ft.Kind() {
			case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64, reflect.String:
				quoted = strings.Contains(","+opts+",", ",string,")
			}
			all = append(all, jsonField{name: name, index: index, tagged: tagged, quoted: quoted})
		}
	}
	walk(t, nil, make(map[reflect.Type]bool))
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.name != b.name {
			return a.name < b.name
		}
		if len(a.index) != len(b.index) {
			return len(a.index) < len(b.index)
		}
		return a.tagged && !b.tagged
	})
	var chosen []jsonField
	for i := 0; i < len(all); {
		j := i + 1
		for j < len(all) && all[j].name == all[i].name {
			j++
		}
		if i+1 == j || len(all[i].index) != len(all[i+1].index) || all[i].tagged != all[i+1].tagged {
			chosen = append(chosen, all[i])
		}
		i = j
	}
	sort.Slice(chosen, func(i, j int) bool {
		a, b := chosen[i].index, chosen[j].index
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return len(a) < len(b)
	})
	f := structFields{exact: make(map[string]jsonField), folded: make(map[string]jsonField)}
	for _, v := range chosen {
		f.exact[v.name] = v
		fold := foldName(v.name)
		if _, ok := f.folded[fold]; !ok {
			f.folded[fold] = v
		}
	}
	actual, _ := fieldsCache.LoadOrStore(t, f)
	return actual.(structFields)
}
func fieldByIndex(v reflect.Value, index []int) (reflect.Value, error) {
	for _, i := range index {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				if !v.CanSet() {
					return reflect.Value{}, fmt.Errorf("json: cannot set embedded pointer to unexported struct: %v", v.Type().Elem())
				}
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v, nil
}

func foldName(s string) string {
	return strings.Map(func(r rune) rune {
		if r < utf8.RuneSelf {
			if r >= 'a' && r <= 'z' {
				return r - 'a' + 'A'
			}
			return r
		}
		for {
			next := unicode.SimpleFold(r)
			if next <= r {
				return next
			}
			r = next
		}
	}, s)
}

func validTagName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", r) {
			return false
		}
	}
	return true
}

// A ,string JSONUnmarshaler receives the unquoted bytes even if those bytes
// are not themselves JSON. Its method owns validation of that representation.
func decodeQuotedCustom(v reflect.Value, raw []byte) (error, bool) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		if v.Type().Implements(unmarshalerType) {
			return customResult(v.Interface().(json.Unmarshaler).UnmarshalJSON(raw)), true
		}
		v = v.Elem()
	}
	if v.CanAddr() && v.Addr().Type().Implements(unmarshalerType) {
		return customResult(v.Addr().Interface().(json.Unmarshaler).UnmarshalJSON(raw)), true
	}
	return nil, false
}
