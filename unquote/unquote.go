// Package unquote decodes escaped JSON string contents using Sonic's native
// byte-preserving semantics. Surrounding quotes are not part of the input.
package unquote

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	nativetypes "github.com/bytedance/sonic/internal/native/types"
)

// String unescapes s. Unpaired UTF-16 surrogates become U+FFFD. Raw bytes,
// including controls and invalid UTF-8, are retained like Sonic's native API.
func String(s string) (string, nativetypes.ParsingError) {
	if !strings.Contains(s, "\\") {
		return s, 0
	}
	dst := make([]byte, 0, len(s))
	if code := decodeStringContent(s, &dst); code != 0 {
		return "", code
	}
	return string(dst), 0
}

// IntoBytes decodes into the caller's storage. Capacity must be at least
// len(s); insufficient capacity returns ERR_EOF without changing the slice.
// On a malformed escape, the original length is preserved, although the
// underlying storage may already contain the successfully decoded prefix.
func IntoBytes(s string, m *[]byte) nativetypes.ParsingError {
	if m == nil {
		return nativetypes.ERR_UNSUPPORT_TYPE
	}
	if cap(*m) < len(s) {
		return nativetypes.ERR_EOF
	}
	dst := (*m)[:0]
	code := decodeStringContent(s, &dst)
	if code == 0 {
		*m = dst
	}
	return code
}

func decodeStringContent(s string, dst *[]byte) nativetypes.ParsingError {
	for i := 0; i < len(s); {
		end := strings.IndexByte(s[i:], '\\')
		if end < 0 {
			*dst = append(*dst, s[i:]...)
			return 0
		}
		end += i
		*dst = append(*dst, s[i:end]...)
		i = end
		if i+1 == len(s) {
			return nativetypes.ERR_EOF
		}
		switch s[i+1] {
		case '"', '\\', '/':
			*dst = append(*dst, s[i+1])
			i += 2
		case 'b':
			*dst = append(*dst, '\b')
			i += 2
		case 'f':
			*dst = append(*dst, '\f')
			i += 2
		case 'n':
			*dst = append(*dst, '\n')
			i += 2
		case 'r':
			*dst = append(*dst, '\r')
			i += 2
		case 't':
			*dst = append(*dst, '\t')
			i += 2
		case 'u':
			r, next, code := decodeUnicodeEscape(s, i)
			if code != 0 {
				return code
			}
			*dst = utf8.AppendRune(*dst, r)
			i = next
		default:
			return nativetypes.ERR_INVALID_ESCAPE
		}
	}
	return 0
}

func decodeUnicodeEscape(s string, i int) (rune, int, nativetypes.ParsingError) {
	if len(s)-i < 6 {
		return 0, 0, nativetypes.ERR_EOF
	}
	r, ok := scanHex4(s, i+2)
	if !ok {
		return 0, 0, nativetypes.ERR_INVALID_CHAR
	}
	next := i + 6
	if r >= 0xD800 && r <= 0xDBFF {
		if next+6 <= len(s) && s[next] == '\\' && s[next+1] == 'u' {
			r2, ok := scanHex4(s, next+2)
			if !ok {
				return 0, 0, nativetypes.ERR_INVALID_CHAR
			}
			if r2 >= 0xDC00 && r2 <= 0xDFFF {
				return utf16.DecodeRune(r, r2), next + 6, 0
			}
		}
		return utf8.RuneError, next, 0
	}
	if r >= 0xDC00 && r <= 0xDFFF {
		return utf8.RuneError, next, 0
	}
	return r, next, 0
}

func scanHex4(s string, i int) (rune, bool) {
	if i+4 > len(s) {
		return 0, false
	}
	var r rune
	for j := 0; j < 4; j++ {
		c := s[i+j]
		var v rune
		switch {
		case c >= '0' && c <= '9':
			v = rune(c - '0')
		case c >= 'a' && c <= 'f':
			v = rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v = rune(c-'A') + 10
		default:
			return 0, false
		}
		r = r*16 + v
	}
	return r, true
}
