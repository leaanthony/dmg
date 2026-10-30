package hfsplus

//go:generate python3 gen_unicode.py
//go:generate gofmt -w unicode_tables.go

// decomposeName implements TN1150's Unicode 3.2 canonical decomposition,
// including HFS's exclusions. Newer Unicode rules would change catalog keys.
// The tables cover only BMP characters to match HFS's UTF-16-unit normalization;
// supplementary characters remain unchanged and stop combining-mark ordering.
func decomposeName(name string) []rune {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 0xAC00 && r <= 0xD7A3:
			s := r - 0xAC00
			out = append(out, 0x1100+s/588, 0x1161+(s%588)/28)
			if t := s % 28; t != 0 {
				out = append(out, 0x11A7+t)
			}
		default:
			if d, ok := hfsDecompositions[r]; ok {
				out = append(out, []rune(d)...)
			} else {
				out = append(out, r)
			}
		}
	}
	// Stable canonical ordering stops at starters (combining class zero).
	for i := 1; i < len(out); i++ {
		c := hfsCombiningClasses[out[i]]
		if c == 0 {
			continue
		}
		for j := i; j > 0 && hfsCombiningClasses[out[j-1]] > c; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
