//go:build sonic_jsonv2 && !sonic_stdjson && goexperiment.jsonv2

package fastjson

import (
	"testing"

	"github.com/bytedance/sonic/ast"
)

func TestJSONV2TagInheritsRootValidationAndGet(t *testing.T) {
	const invalid = `[1] trailing`
	if Valid([]byte(invalid)) {
		t.Fatal("Valid accepted trailing data under sonic_jsonv2")
	}

	tests := map[string]func() error{
		"Get":               func() error { _, err := Get([]byte(invalid)); return err },
		"GetFromString":     func() error { _, err := GetFromString(invalid); return err },
		"GetCopyFromString": func() error { _, err := GetCopyFromString(invalid); return err },
		"GetWithOptions": func() error {
			_, err := GetWithOptions([]byte(invalid), ast.SearchOptions{ValidateJSON: true})
			return err
		},
	}
	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatalf("%s accepted trailing data under sonic_jsonv2", name)
			}
		})
	}
}
