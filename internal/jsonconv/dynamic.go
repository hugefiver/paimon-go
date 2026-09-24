package jsonconv

import (
	"encoding/json"
	"reflect"
)

// UnmarshalDynamic decodes plain dynamic targets using float64 JSON numbers.
// Concrete pointers inside interfaces and existing map state retain the standard
// decoder's dispatch and update semantics. Only this bounded entry point opts
// the shared token cursor into float64 mode.
func UnmarshalDynamic(data []byte, dst any) error {
	switch out := dst.(type) {
	case *any:
		if out == nil {
			return json.Unmarshal(data, dst)
		}
		if *out != nil && reflect.ValueOf(*out).Kind() == reflect.Pointer {
			if self, ok := (*out).(*any); !ok || self != out {
				return json.Unmarshal(data, dst)
			}
		}
	case *map[string]any:
		if out == nil || len(*out) != 0 {
			return json.Unmarshal(data, dst)
		}
	default:
		return json.Unmarshal(data, dst)
	}
	if !json.Valid(data) {
		return json.Unmarshal(data, dst)
	}
	d := numberDecoder{data: data, useFloat64: true}
	d.space()
	switch out := dst.(type) {
	case *any:
		*out = nil
		value, err := d.anyValue()
		if err == nil {
			*out = value
		}
		return err
	case *map[string]any:
		if data[d.pos] == 'n' {
			*out = nil
			return nil
		}
		if data[d.pos] != '{' {
			return json.Unmarshal(data, dst)
		}
		if *out == nil {
			*out = make(map[string]any)
		}
		d.pos++
		d.space()
		for d.data[d.pos] != '}' {
			key := d.key()
			value, err := d.anyValue()
			(*out)[key] = value
			if err != nil {
				return err
			}
			d.separator()
		}
		return nil
	}
	return nil
}
