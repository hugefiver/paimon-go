// Package unquote mirrors the public surface of Sonic's v1.15.2 unquote
// package.
//
// The entry points String and IntoBytes decode the *contents* of a JSON
// string literal: the input is the text between the surrounding JSON
// quotes, not a fully quoted JSON literal. The decoder accepts ordinary
// UTF-8 bytes unchanged and decodes the JSON escape sequences "\\", "\"",
// "\/", "\b", "\f", "\n", "\r", "\t", and "\uXXXX", combining valid
// UTF-16 surrogate pairs into their single rune form.
//
// Errors are reported as
// github.com/bytedance/sonic/internal/native/types.ParsingError codes:
//
//   - ERR_INVALID_ESCAPE: unknown escape letter, truncated escape
//     sequence, or malformed \uXXXX hex digits.
//   - ERR_INVALID_UNICODE: a \uXXXX escape is a surrogate that is not
//     followed by a valid surrogate pair partner (a high surrogate not
//     followed by a low surrogate, or a low surrogate without a high
//     surrogate).
//   - ERR_INVALID_CHAR: a raw control character with code point < 0x20.
//   - ERR_INVALID_UTF8: an invalid UTF-8 byte sequence outside any escape.
//   - ERR_UNSUPPORT_TYPE: IntoBytes was called with a nil destination
//     pointer.
package unquote

import (
	"unicode/utf16"
	"unicode/utf8"

	nativetypes "github.com/bytedance/sonic/internal/native/types"
)

// String unescapes the contents of a JSON string literal. The input is
// the text between the surrounding JSON quotes; the returned string
// contains the decoded bytes. A non-zero ParsingError is returned when
// the input is malformed.
func String(s string) (string, nativetypes.ParsingError) {
	dst := make([]byte, 0, len(s))
	if code := decodeStringContent(s, &dst); code != 0 {
		return "", code
	}
	return string(dst), 0
}

// IntoBytes unescapes the contents of a JSON string literal into the
// buffer pointed to by m. The destination slice is reset to length 0
// before any decoded bytes are appended, so callers can reuse the same
// backing array across calls. If m is nil, IntoBytes returns
// ERR_UNSUPPORT_TYPE rather than panicking.
func IntoBytes(s string, m *[]byte) nativetypes.ParsingError {
	if m == nil {
		return nativetypes.ERR_UNSUPPORT_TYPE
	}
	*m = (*m)[:0]
	return decodeStringContent(s, m)
}

// decodeStringContent walks s as JSON string contents and appends the
// decoded bytes to *dst. It returns a non-zero ParsingError on the first
// malformed byte or escape encountered.
func decodeStringContent(s string, dst *[]byte) nativetypes.ParsingError {
	i := 0
	for i < len(s) {
		c := s[i]
		if c < 0x20 {
			return nativetypes.ERR_INVALID_CHAR
		}
		if c != '\\' {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 && s[i] >= 0x80 {
				return nativetypes.ERR_INVALID_UTF8
			}
			*dst = append(*dst, s[i:i+size]...)
			i += size
			continue
		}
		if i+1 >= len(s) {
			return nativetypes.ERR_INVALID_ESCAPE
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

// decodeUnicodeEscape decodes a \uXXXX escape beginning at s[i] (s[i] must
// be '\\'). It also consumes a following \uYYYY escape when the first
// escape is a UTF-16 high surrogate. It returns the decoded rune, the
// index in s just past the consumed escape(s), and a non-zero
// ParsingError when the escape is malformed or the surrogate pairing is
// invalid.
func decodeUnicodeEscape(s string, i int) (rune, int, nativetypes.ParsingError) {
	r, ok := scanHex4(s, i+2)
	if !ok {
		return 0, 0, nativetypes.ERR_INVALID_ESCAPE
	}
	next := i + 6
	if utf16.IsSurrogate(r) {
		if !utf16.IsSurrogate(r) || r < 0xD800 || r > 0xDBFF {
			// A lone low surrogate: invalid Unicode.
			return 0, 0, nativetypes.ERR_INVALID_UNICODE
		}
		// Expect a following \uYYYY low surrogate.
		if next+6 > len(s) || s[next] != '\\' || s[next+1] != 'u' {
			return 0, 0, nativetypes.ERR_INVALID_UNICODE
		}
		r2, ok := scanHex4(s, next+2)
		if !ok {
			return 0, 0, nativetypes.ERR_INVALID_ESCAPE
		}
		if r2 < 0xDC00 || r2 > 0xDFFF {
			return 0, 0, nativetypes.ERR_INVALID_UNICODE
		}
		decoded := utf16.DecodeRune(r, r2)
		if decoded == 0xFFFD {
			return 0, 0, nativetypes.ERR_INVALID_UNICODE
		}
		return decoded, next + 6, 0
	}
	return r, next, 0
}

// scanHex4 decodes a 4-digit hexadecimal sequence beginning at s[i]. It
// requires s[i:i+4] to be valid and to consist entirely of hex digits.
// It returns the decoded value and true, or 0 and false on any error.
func scanHex4(s string, i int) (rune, bool) {
	if i+4 > len(s) {
		return 0, false
	}
	var r rune
	for j := 0; j < 4; j++ {
		c := s[i+j]
		var v rune
		switch {
		case '0' <= c && c <= '9':
			v = rune(c - '0')
		case 'a' <= c && c <= 'f':
			v = rune(c-'a') + 10
		case 'A' <= c && c <= 'F':
			v = rune(c-'A') + 10
		default:
			return 0, false
		}
		r = r*16 + v
	}
	return r, true
}
