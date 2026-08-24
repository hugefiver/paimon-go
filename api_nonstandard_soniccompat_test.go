//go:build !sonic_stdjson && !sonic_jsonv2

package sonic

import "testing"

func TestRootAcceptsRawControlByteInStringLikeSonic(t *testing.T) {
	data := []byte{'{', '"', 0x11, 'x', '"', ':', '1', '}'}
	if !Valid(data) {
		t.Fatalf("Valid(%q) = false, want true for Sonic-compatible raw control string", data)
	}
	var out map[string]interface{}
	if err := Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v, want nil", data, err)
	}
	if _, ok := out[string([]byte{0x11, 'x'})]; !ok {
		t.Fatalf("decoded keys = %#v, want raw-control key", out)
	}
}

func TestRootGetReturnsFirstValueBeforeTrailingGarbageLikeSonic(t *testing.T) {
	n, err := Get([]byte(`[1,true]x"",`))
	if err != nil {
		t.Fatalf("Get with trailing garbage error = %v, want nil", err)
	}
	raw, err := n.Raw()
	if err != nil {
		t.Fatalf("Raw() error = %v", err)
	}
	if raw != `[1,true]` {
		t.Fatalf("Raw() = %q, want first JSON value", raw)
	}
}
