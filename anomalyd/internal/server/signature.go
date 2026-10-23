package server

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// Signature is a named RE2 pattern for a known-bad log template (--signature-file).
type Signature struct {
	Name string
	Re   *regexp.Regexp
}

var sigName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// LoadSignatures reads a signature file; see ParseSignatures.
func LoadSignatures(path string) ([]Signature, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseSignatures(f)
}

// ParseSignatures reads one "name: regex" per line. Lines starting with '#' and blank lines are
// skipped. A regex sees the masked template text, so numbers already read <NUM>:
//
//	pool-exhausted: db pool exhausted: timeout after <NUM>
//	oom: (?i)OutOfMemoryError|out of memory
func ParseSignatures(r io.Reader) ([]Signature, error) {
	var out []Signature
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		name, expr, ok := strings.Cut(line, ":")
		name, expr = strings.TrimSpace(name), strings.TrimSpace(expr)
		if !ok || !sigName.MatchString(name) || expr == "" {
			return nil, fmt.Errorf("line %d: want \"name: regex\" with a name of [A-Za-z0-9_.-]", n)
		}
		if seen[name] {
			return nil, fmt.Errorf("line %d: duplicate signature %q", n, name)
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		seen[name] = true
		out = append(out, Signature{Name: name, Re: re})
	}
	return out, sc.Err()
}
