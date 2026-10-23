package drain

import (
	"slices"
	"strings"
	"testing"
)

// FuzzTokenize checks the masker on arbitrary lines: one token per whitespace-separated
// field, no whitespace inside a token, clean fields untouched, and no state leaking between
// calls through the reused buffers. Every line then goes through a small miner.
func FuzzTokenize(f *testing.F) {
	for _, s := range []string{
		"", "  GET /x  200\t12ms ", "client=10.0.0.1:8080 took=1606ms",
		"session=6ba7b810-9dad-11d1-80b4-00c04fd430c8, q_hash=00ff00ff00ff00ff 0x1f",
		"mail=john.doe+x@example.co.uk; a@b.c,d@e.f @home user42 12%abc 1.2.3.4.5",
		"\xff\xfe 0x \x00 9999999999999999999999 é 1.", strings.Repeat("a1 ", 50),
	} {
		f.Add(s)
	}
	tk, warm := NewTokenizer(), NewTokenizer()
	m := NewMiner(Config{Depth: 4, SimTh: 0.4, MaxChildren: 3, MaxClusters: 50})
	f.Fuzz(func(t *testing.T, line string) {
		fields := strings.FieldsFunc(line, func(r rune) bool { return r < 0x80 && isSpace(byte(r)) })
		got := slices.Clone(tk.Tokenize(line))
		if len(got) != len(fields) {
			t.Fatalf("%q: %d tokens for %d fields: %q", line, len(got), len(fields), got)
		}
		for i, tok := range got {
			if tok == "" || strings.ContainsAny(tok, " \t\n\r\v\f") {
				t.Fatalf("%q: bad token %q", line, tok)
			}
			if !strings.ContainsAny(fields[i], "0123456789@") && tok != fields[i] {
				t.Fatalf("%q: clean field %q masked to %q", line, fields[i], tok)
			}
		}
		warm.Tokenize(strings.Repeat("x=1 ", 8) + "a@b.co 0xff") // dirty the buffers
		if again := warm.Tokenize(line); !slices.Equal(got, again) {
			t.Fatalf("%q: %q then %q", line, got, again)
		}
		c, _ := m.AddLine(line)
		if len(c.Tokens) != len(got) || m.Get(c.ID) != c || m.Len() > 50 {
			t.Fatalf("%q: cluster %v (%d tokens), %d templates", line, c.Tokens, len(got), m.Len())
		}
	})
}
