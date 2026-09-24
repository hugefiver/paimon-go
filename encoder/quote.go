package encoder

import "strings"

// quoteJSON follows Sonic's byte-preserving quoting rules. strconv.Quote is
// unsuitable here: its Go-only \x, \a and \v escapes are not JSON escapes.
func quoteJSON(s string) string {
	const hex = "0123456789abcdef"
	var out strings.Builder
	out.Grow(len(s) + 2)
	out.WriteByte('"')
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		out.WriteString(s[start:i])
		switch c {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteByte(c)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			out.WriteString(`\u00`)
			out.WriteByte(hex[c>>4])
			out.WriteByte(hex[c&15])
		}
		start = i + 1
	}
	out.WriteString(s[start:])
	out.WriteByte('"')
	return out.String()
}
