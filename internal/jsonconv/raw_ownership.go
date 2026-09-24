package jsonconv

import (
	"reflect"
	"strings"
	"sync"
)

var rawOwnershipCache sync.Map

// RetainsRawInput reports whether decoding this type may populate Sonic's
// NoCopyRawMessage. Interfaces are conservative because a caller can prefill
// them with an arbitrary pointer. Ordinary concrete types are cached once.
func RetainsRawInput(t reflect.Type) bool {
	if t == nil {
		return false
	}
	if cached, ok := rawOwnershipCache.Load(t); ok {
		return cached.(bool)
	}
	result := retainsRawInput(t, make(map[reflect.Type]bool))
	rawOwnershipCache.Store(t, result)
	return result
}

func retainsRawInput(t reflect.Type, seen map[reflect.Type]bool) bool {
	if t.Name() == "NoCopyRawMessage" && t.PkgPath() == "github.com/bytedance/sonic" {
		return true
	}
	if seen[t] {
		return false
	}
	seen[t] = true
	defer delete(seen, t)
	switch t.Kind() {
	case reflect.Interface:
		return true
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return retainsRawInput(t.Elem(), seen)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" && !field.Anonymous {
				continue
			}
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name != "-" && retainsRawInput(field.Type, seen) {
				return true
			}
		}
	}
	return false
}
