package unquote

import (
	nativetypes "github.com/bytedance/sonic/internal/native/types"
	"testing"
)

// Expected results were checked against Sonic v1.15.2 on Go 1.26.7/amd64.
func TestNativeUnquoteValues(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""}, {"plain", "plain"}, {"héllo 世界", "héllo 世界"},
		{`\"`, "\""}, {`\\`, "\\"}, {`\/`, "/"}, {`\b`, "\b"}, {`\f`, "\f"}, {`\n`, "\n"}, {`\r`, "\r"}, {`\t`, "\t"},
		{`a\tb\n\\c\"d`, "a\tb\n\\c\"d"}, {`\u0041`, "A"}, {`\u4e16`, "世"}, {`\uD83D\uDE00`, "😀"},
		{`\uD800`, "�"}, {`\uDC00`, "�"}, {`\uD800\u0041`, "�A"}, {`\uD800abc`, "�abc"}, {`\uD800\uD800`, "��"},
		{"a\x00b", "a\x00b"}, {"a\tb", "a\tb"}, {"a\nb", "a\nb"}, {"\xff\xfe", "\xff\xfe"}, {`a"b`, `a"b`},
	}
	for _, tt := range cases {
		t.Run(tt.in, func(t *testing.T) {
			got, code := String(tt.in)
			if code != 0 || got != tt.want {
				t.Fatalf("String(%q)=%q,%d; want %q,0", tt.in, got, code, tt.want)
			}
			dst := make([]byte, 0, len(tt.in))
			if code = IntoBytes(tt.in, &dst); code != 0 || string(dst) != tt.want {
				t.Fatalf("IntoBytes(%q)=%q,%d; want %q,0", tt.in, dst, code, tt.want)
			}
		})
	}
}

func TestNativeUnquoteErrors(t *testing.T) {
	cases := []struct {
		in   string
		code nativetypes.ParsingError
	}{
		{`\q`, nativetypes.ERR_INVALID_ESCAPE}, {`\x`, nativetypes.ERR_INVALID_ESCAPE},
		{`a\`, nativetypes.ERR_EOF}, {`\u`, nativetypes.ERR_EOF}, {`\u00`, nativetypes.ERR_EOF},
		{`\uZZZZ`, nativetypes.ERR_INVALID_CHAR}, {`\uD800\uZZZZ`, nativetypes.ERR_INVALID_CHAR},
	}
	for _, tt := range cases {
		t.Run(tt.in, func(t *testing.T) {
			got, code := String(tt.in)
			if got != "" || code != tt.code {
				t.Fatalf("String(%q)=%q,%d; want empty,%d", tt.in, got, code, tt.code)
			}
			dst := make([]byte, 3, 64)
			copy(dst, "old")
			if code = IntoBytes(tt.in, &dst); code != tt.code || len(dst) != 3 {
				t.Fatalf("IntoBytes(%q) len=%d code=%d; want 3,%d", tt.in, len(dst), code, tt.code)
			}
		})
	}
}

func TestIntoBytesCapacityContract(t *testing.T) {
	dst := []byte("old")
	if code := IntoBytes("longer", &dst); code != nativetypes.ERR_EOF || string(dst) != "old" {
		t.Fatalf("insufficient capacity=%q,%d", dst, code)
	}
	dst = make([]byte, 9, 16)
	if code := IntoBytes(`x`, &dst); code != 0 || string(dst) != "x" || cap(dst) != 16 {
		t.Fatalf("reuse=%q,%d cap=%d", dst, code, cap(dst))
	}
	var empty []byte
	if code := IntoBytes("", &empty); code != 0 || len(empty) != 0 {
		t.Fatalf("empty=%q,%d", empty, code)
	}
	if code := IntoBytes(`x`, nil); code != nativetypes.ERR_UNSUPPORT_TYPE {
		t.Fatalf("nil pointer code=%d", code)
	}
}
