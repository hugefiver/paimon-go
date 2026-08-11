package jsonconv

import (
	"encoding/json"
	"reflect"
	"strconv"
)

// ConvertNumbersToInt64 walks ptr and converts json.Number values to int64
// when they contain a base-10 integer that fits in int64. Numbers that are
// fractional or overflow int64 are left as json.Number.
func ConvertNumbersToInt64(ptr interface{}) {
	v := reflect.ValueOf(ptr)
	if !v.IsValid() || v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}
	convertValue(v.Elem())
}

func convertValue(v reflect.Value) {
	if !v.IsValid() || !v.CanSet() && v.Kind() != reflect.Map {
		return
	}
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		inner := v.Elem()
		if num, ok := inner.Interface().(json.Number); ok {
			if i, err := strconv.ParseInt(string(num), 10, 64); err == nil {
				v.Set(reflect.ValueOf(i))
			}
			return
		}
		switch inner.Kind() {
		case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct, reflect.Ptr, reflect.Interface:
			tmp := reflect.New(inner.Type()).Elem()
			tmp.Set(inner)
			convertValue(tmp)
			v.Set(tmp)
		}
	case reflect.Ptr:
		if !v.IsNil() {
			convertValue(v.Elem())
		}
	case reflect.Map:
		if v.IsNil() {
			return
		}
		iter := v.MapRange()
		for iter.Next() {
			newVal := reflect.New(v.Type().Elem()).Elem()
			newVal.Set(iter.Value())
			convertValue(newVal)
			v.SetMapIndex(iter.Key(), newVal)
		}
	case reflect.Slice:
		if v.IsNil() {
			return
		}
		for i := 0; i < v.Len(); i++ {
			convertValue(v.Index(i))
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			convertValue(v.Index(i))
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.CanSet() {
				convertValue(field)
			}
		}
	}
}
