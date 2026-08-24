//go:build !sonic_jsonv2

package sonic_test

import (
	"encoding/json"
	"testing"

	"github.com/bytedance/sonic/internal/compatmode"
)

func validParityOracle(data []byte) (bool, string) {
	if compatmode.StdJSON {
		return len(data) > 0 && json.Valid(data), "stdjson"
	}

	return defaultValidOracle(data), "sonic-compatible"
}

func TestDefaultValidOracle(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{name: "ordinary JSON", data: []byte(`{"a":[true,null,1.25,"ok"]}`), want: true},
		{name: "empty", data: nil, want: false},
		{name: "trailing data", data: []byte(`{"a":1}extra`), want: false},
		{name: "leading zero", data: []byte(`{"a":01}`), want: false},
		{name: "malformed exponent", data: []byte(`{"a":1e}`), want: false},
		{name: "unknown escape", data: []byte(`{"a":"\q"}`), want: false},
		{name: "truncated escape", data: []byte(`{"a":"\`), want: false},
		{name: "control outside string", data: []byte{'{', 0x1f, '"', 'a', '"', ':', '1', '}'}, want: false},
		{name: "unterminated string", data: []byte(`{"a":"unterminated`), want: false},
		{name: "raw controls in string key and value", data: []byte{'{', '"', 0x00, 0x01, 0x11, 0x1f, '"', ':', '"', 0x00, 0x01, 0x11, 0x1f, '"', '}'}, want: true},
		{name: "escaped controls", data: []byte(`{"a":"\u0000\u0001\u0011\u001f\b\f\n\r\t"}`), want: true},
		{name: "invalid UTF-8 string", data: []byte{'{', '"', 'a', '"', ':', '"', 0xff, '"', '}'}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultValidOracle(tt.data); got != tt.want {
				t.Fatalf("defaultValidOracle(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

func defaultValidOracle(data []byte) bool {
	const hex = "0123456789abcdef"

	normalized := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for _, b := range data {
		if !inString {
			if b < 0x20 && b != ' ' && b != '\t' && b != '\n' && b != '\r' {
				return false
			}
			normalized = append(normalized, b)
			if b == '"' {
				inString = true
			}
			continue
		}

		if escaped {
			normalized = append(normalized, b)
			escaped = false
			continue
		}

		switch b {
		case '\\':
			normalized = append(normalized, b)
			escaped = true
		case '"':
			normalized = append(normalized, b)
			inString = false
		default:
			if b < 0x20 {
				normalized = append(normalized, '\\', 'u', '0', '0', hex[b>>4], hex[b&0x0f])
				continue
			}
			normalized = append(normalized, b)
		}
	}

	return !inString && !escaped && json.Valid(normalized)
}
