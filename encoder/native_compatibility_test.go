package encoder

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/bytedance/sonic/decoder"
	nativetypes "github.com/bytedance/sonic/internal/native/types"
)

func TestNativeQuoteControls(t *testing.T) {
	for i := 0; i < 32; i++ {
		s := string(byte(i))
		quoted := Quote(s)
		var out string
		if err := json.Unmarshal([]byte(quoted), &out); err != nil || out != s {
			t.Fatalf("byte %d: %q -> %q %v", i, quoted, out, err)
		}
	}
	if got := Quote("\xff"); got != "\"\xff\"" {
		t.Fatalf("native invalid UTF-8 preservation=%q", got)
	}
}

func TestNativeValidationPositions(t *testing.T) {
	cases := []struct {
		src   string
		valid bool
		pos   int
	}{
		{"", false, -1}, {" \n ", false, 3}, {`{"a":}`, false, 5}, {`[1,]`, false, 3},
		{`true false`, false, 5}, {`trX`, false, 2}, {`[falX]`, false, 3}, {`1e+`, false, 1},
		{`truX`, false, 2}, {`nan`, false, 2}, {`1..2`, false, 1},
		{`{"a"}`, false, 4},
		{strings.Repeat("[", 4096) + "1..2" + strings.Repeat("]", 4096), false, 4097},
		{`"\q"`, true, 0}, {`"\uZZZZ"`, true, 0}, {"\"x\ny\"", true, 0},
	}
	for _, tt := range cases {
		if ok, pos := Valid([]byte(tt.src)); ok != tt.valid || pos != tt.pos {
			t.Fatalf("Valid(%q)=%v,%d; want %v,%d", tt.src, ok, pos, tt.valid, tt.pos)
		}
	}
}

func TestNativeValidationDepthErrorPositions(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  string
		pos  int
	}{
		{"array at limit with nested array", strings.Repeat("[", 4096) + "[]" + strings.Repeat("]", 4096), 4096},
		{"object at limit with scalar", strings.Repeat(`{"a":`, 4096) + "0" + strings.Repeat("}", 4096), 20478},
		{"object at limit with malformed number", strings.Repeat(`{"a":`, 4096) + "1..2" + strings.Repeat("}", 4096), 20478},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, cursor := decoder.Skip([]byte(tt.src))
			if code != -int(nativetypes.ERR_RECURSE_EXCEED_MAX) || cursor-1 != tt.pos {
				t.Fatalf("Skip code=%d cursor=%d; want recursion error and cursor=%d", code, cursor, tt.pos+1)
			}
			if ok, pos := Valid([]byte(tt.src)); ok || pos != tt.pos {
				t.Fatalf("Valid=(%t,%d); want (false,%d)", ok, pos, tt.pos)
			}
		})
	}
}

func TestEncodeIntoUsesExactCapacity(t *testing.T) {
	buf := make([]byte, 2, 5)
	copy(buf, "p:")
	base := &buf[0]
	if err := EncodeInto(&buf, 123, 0); err != nil || string(buf) != "p:123" || &buf[0] != base {
		t.Fatalf("append=%q err=%v reused=%v", buf, err, &buf[0] == base)
	}
	before := string(buf)
	if err := EncodeInto(&buf, make(chan int), 0); err == nil || string(buf) != before {
		t.Fatalf("failed encode changed destination: %q %v", buf, err)
	}
}

var benchmarkValue = struct {
	ID   int
	Name string
}{42, "sonic"}

func BenchmarkEncodeIntoReusedBuffer(b *testing.B) {
	buf := make([]byte, 0, 4096)
	b.ReportAllocs()
	for b.Loop() {
		buf = buf[:0]
		if err := EncodeInto(&buf, &benchmarkValue, 0); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkStreamEncoderReusedBuffer(b *testing.B) {
	e := NewStreamEncoder(io.Discard)
	b.ReportAllocs()
	for b.Loop() {
		if err := e.Encode(&benchmarkValue); err != nil {
			b.Fatal(err)
		}
	}
}
