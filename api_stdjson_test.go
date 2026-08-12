//go:build sonic_stdjson

package sonic

import "testing"

func TestStdJSONBuildTagUsesStrictRawJSONSemantics(t *testing.T) {
	rawControl := []byte{'{', '"', 0x11, 'x', '"', ':', '1', '}'}
	if Valid(rawControl) {
		t.Fatalf("Valid(raw control string) = true, want false under sonic_stdjson")
	}
	var out map[string]interface{}
	if err := Unmarshal(rawControl, &out); err == nil {
		t.Fatalf("Unmarshal(raw control string) error = nil, want strict stdjson error")
	}
	if _, err := Get([]byte(`[1,true]x"",`)); err == nil {
		t.Fatalf("Get with trailing garbage error = nil, want strict stdjson error")
	}
	n, err := Get([]byte(`{"users":[{"id":1},{"id":2}]}`), "users", 1, "id")
	if err != nil {
		t.Fatalf("Get valid path error = %v", err)
	}
	if got, err := n.Int64(); err != nil || got != 2 {
		t.Fatalf("Get valid path = %d, %v; want 2, nil", got, err)
	}
}
