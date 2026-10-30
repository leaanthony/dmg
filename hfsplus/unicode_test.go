package hfsplus

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func TestCatalogUnicodeNames(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"ASCII.txt", "ASCII.txt"},
		// Supplementary characters remain intact and stop combining-mark ordering.
		{"\U0001d15e.txt", "\U0001d15e.txt"},
		{"a\u0301\U0001d165\u0327", "a\u0301\U0001d165\u0327"},
		{"caf\u00e9.txt", "cafe\u0301.txt"},
		{"cafe\u0301.txt", "cafe\u0301.txt"},
		{"\uac00\uac01.txt", "\u1100\u1161\u1100\u1161\u11a8.txt"},
		{"\u01fa", "A\u030a\u0301"},
		{"a\u0301\u0327", "a\u0327\u0301"},
		{"\u212b\uf900\U0002f800\u00e9", "\u212b\uf900\U0002f800e\u0301"},
		// Characters introduced after Unicode 3.2 must remain unchanged.
		{"\u1b06a\u1ab0\u0327", "\u1b06a\u1ab0\u0327"},
		// HFS decomposition must not insert stream-safe CGJ characters.
		{"a" + strings.Repeat("\u0301", 40), "a" + strings.Repeat("\u0301", 40)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := func(data []byte) {
				t.Helper()
				n := int(be.Uint16(data[:2]))
				if len(data) != 2+2*n {
					t.Fatalf("invalid name length: %d units in %d bytes", n, len(data))
				}
				u := make([]uint16, n)
				for i := range u {
					u[i] = be.Uint16(data[2+2*i:])
				}
				if got := string(utf16.Decode(u)); got != tc.want {
					t.Fatalf("name = %q, want %q", got, tc.want)
				}
			}
			check(catalogKey(2, tc.name)[6:])
			for _, kind := range []uint16{recFileThread, recFolderThread} {
				record := catalogThreadRecord(16, kind, 2, tc.name)
				keyLen := int(be.Uint16(record[:2])) + 2
				check(record[keyLen+8:])
			}
		})
	}
}

func TestBuildRejectsInvalidDecomposedNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		vol  *Volume
	}{
		{"duplicate", &Volume{Name: "Test", CaseSensitive: true, Children: []*Entry{
			{Name: "caf\u00e9", Kind: KindFile}, {Name: "cafe\u0301", Kind: KindDir},
		}}},
		{"long entry", &Volume{Name: "Test", CaseSensitive: true, Children: []*Entry{
			{Name: strings.Repeat("\u00e9", 128), Kind: KindFile},
		}}},
		{"long volume", &Volume{Name: strings.Repeat("\u00e9", 128), CaseSensitive: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Build(tc.vol); err == nil {
				t.Fatal("expected invalid catalog name to be rejected")
			}
		})
	}
}
