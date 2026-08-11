//go:build goexperiment.jsonv2

package stdjsonv2

import (
	"encoding/json"
	"testing"

	root "github.com/bytedance/sonic"
)

// TestBackendAgreementMarshal compares the root sonic Marshal with the
// stdjsonv2 Marshal for a few deterministic cases under GOEXPERIMENT=jsonv2.
func TestBackendAgreementMarshal(t *testing.T) {
	cases := []interface{}{
		nil,
		true,
		false,
		42,
		-1,
		3.14,
		"hello",
		[]int{1, 2, 3},
		map[string]int{"a": 1, "b": 2},
		struct {
			A int    `json:"a"`
			B string `json:"b"`
		}{A: 1, B: "x"},
	}
	for _, v := range cases {
		rootB, err := root.Marshal(v)
		if err != nil {
			t.Fatalf("root Marshal error: %v (val=%v)", err, v)
		}
		v2B, err := Marshal(v)
		if err != nil {
			t.Fatalf("stdjsonv2 Marshal error: %v (val=%v)", err, v)
		}
		// Both should be valid JSON.
		if !json.Valid(rootB) {
			t.Fatalf("root Marshal invalid: %q (val=%v)", rootB, v)
		}
		if !json.Valid(v2B) {
			t.Fatalf("stdjsonv2 Marshal invalid: %q (val=%v)", v2B, v)
		}
		// Compare by unmarshaling to interface{} and checking equality.
		var ri, vi interface{}
		if err := json.Unmarshal(rootB, &ri); err != nil {
			t.Fatalf("root unmarshal error: %v (b=%q)", err, rootB)
		}
		if err := json.Unmarshal(v2B, &vi); err != nil {
			t.Fatalf("stdjsonv2 unmarshal error: %v (b=%q)", err, v2B)
		}
		if !equalJSON(ri, vi) {
			t.Fatalf("backend mismatch: root=%q stdjsonv2=%q (val=%v)", rootB, v2B, v)
		}
	}
}

// TestBackendAgreementValid compares Valid parity.
func TestBackendAgreementValid(t *testing.T) {
	cases := [][]byte{
		[]byte(``),
		[]byte(`null`),
		[]byte(`{"a":1}`),
		[]byte(`[1,2,3]`),
		[]byte(`"x"`),
		[]byte(`{`),
		[]byte(`tru`),
		[]byte(`123abc`),
	}
	for _, data := range cases {
		gotRoot := root.Valid(data)
		gotV2 := Valid(data)
		if gotRoot != gotV2 {
			t.Fatalf("Valid mismatch: root=%v stdjsonv2=%v (data=%q)", gotRoot, gotV2, data)
		}
	}
}

// TestBackendAgreementUnmarshal compares Unmarshal results.
func TestBackendAgreementUnmarshal(t *testing.T) {
	cases := []string{
		`null`,
		`true`,
		`42`,
		`3.14`,
		`"hello"`,
		`[1,2,3]`,
		`{"a":1,"b":2}`,
	}
	for _, src := range cases {
		var rootOut, v2Out interface{}
		if err := root.Unmarshal([]byte(src), &rootOut); err != nil {
			t.Fatalf("root Unmarshal error: %v (src=%q)", err, src)
		}
		if err := Unmarshal([]byte(src), &v2Out); err != nil {
			t.Fatalf("stdjsonv2 Unmarshal error: %v (src=%q)", err, src)
		}
		if !equalJSON(rootOut, v2Out) {
			t.Fatalf("Unmarshal mismatch: root=%v stdjsonv2=%v (src=%q)", rootOut, v2Out, src)
		}
	}
}

// equalJSON compares two interface{} values decoded from JSON for semantic
// equality (numbers are compared as float64).
func equalJSON(a, b interface{}) bool {
	switch av := a.(type) {
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case float64:
		switch bv := b.(type) {
		case float64:
			return av == bv
		case int:
			return av == float64(bv)
		case int64:
			return av == float64(bv)
		}
	case int:
		switch bv := b.(type) {
		case int:
			return av == bv
		case float64:
			return float64(av) == bv
		case int64:
			return int64(av) == bv
		}
	case int64:
		switch bv := b.(type) {
		case int64:
			return av == bv
		case float64:
			return float64(av) == bv
		case int:
			return av == int64(bv)
		}
	case []interface{}:
		bv, ok := b.([]interface{})
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !equalJSON(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]interface{}:
		bv, ok := b.(map[string]interface{})
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			if !equalJSON(v, bv[k]) {
				return false
			}
		}
		return true
	case nil:
		return b == nil
	}
	return false
}
