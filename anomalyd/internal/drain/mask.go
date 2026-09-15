// Package drain implements the Drain log-template miner (He et al., ICWS 2017) with the
// same tree, similarity and merge rules as Drain3, plus a single-pass token masker.
//
// The rules are ported from Drain3 (https://github.com/logpai/Drain3), MIT License,
// Copyright (c) 2020-2022 International Business Machines and the Drain3 project
// contributors. The full notice is in NOTICE at the repository root.
package drain

// Mask tokens. Same names as the Drain3 masking config in poc/logs/drain3_exporter.py.
const (
	Param     = "<*>"
	maskUUID  = "<UUID>"
	maskIP    = "<IP>"
	maskHex   = "<HEX>"
	maskNum   = "<NUM>"
	maskEmail = "<EMAIL>"
)

// Tokenizer splits a line on ASCII whitespace and masks variable parts of each token in one
// pass, without regular expressions. Masking follows the Drain3 regexes used in the Python
// PoC: UUID, IPv4, 0x-hex or 12+ hex chars, and numbers with an optional unit, each bounded
// by non-alphanumeric characters. E-mail addresses are masked too.
//
// Returned tokens are substrings of the input or interned mask strings; a Tokenizer is not
// safe for concurrent use.
type Tokenizer struct {
	buf    []byte
	toks   []string
	intern map[string]string
}

func NewTokenizer() *Tokenizer { return &Tokenizer{intern: make(map[string]string, 1024)} }

// Tokenize returns the masked tokens of line. The slice is reused by the next call.
func (t *Tokenizer) Tokenize(line string) []string {
	t.toks = t.toks[:0]
	i, n := 0, len(line)
	for i < n {
		for i < n && isSpace(line[i]) {
			i++
		}
		if i >= n {
			break
		}
		j, dirty := i, false
		for j < n && !isSpace(line[j]) {
			if c := line[j]; isDigit(c) || c == '@' {
				dirty = true
			}
			j++
		}
		tok := line[i:j]
		if dirty {
			tok = t.mask(tok)
		}
		t.toks = append(t.toks, tok)
		i = j
	}
	return t.toks
}

func (t *Tokenizer) mask(tok string) string {
	b := t.buf[:0]
	changed := false
	if lo, hi, ok := findEmail(tok); ok {
		b = append(b, tok[:lo]...)
		b = append(b, maskEmail...)
		tok = string(append(b, tok[hi:]...))
		b, changed = t.buf[:0], true
	}
	n := len(tok)
	for i := 0; i < n; {
		c := tok[i]
		if !isAlnum(c) {
			b = append(b, c)
			i++
			continue
		}
		// i is the start of an alphanumeric run, so the left boundary always holds.
		var l int
		var m string
		if l = matchUUID(tok, i); l > 0 {
			m = maskUUID
		} else if l = matchIPv4(tok, i); l > 0 {
			m = maskIP
		} else if l = matchHex(tok, i); l > 0 {
			m = maskHex
		} else if l = matchNum(tok, i); l > 0 {
			m = maskNum
		}
		if l > 0 {
			b = append(b, m...)
			i += l
			changed = true
			continue
		}
		j := i
		for j < n && isAlnum(tok[j]) {
			j++
		}
		b = append(b, tok[i:j]...)
		i = j
	}
	t.buf = b
	if !changed {
		return tok
	}
	if s, ok := t.intern[string(b)]; ok { // no allocation for the lookup
		return s
	}
	if len(t.intern) > 1<<16 {
		clear(t.intern)
	}
	s := string(b)
	t.intern[s] = s
	return s
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'
}
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool { return (c|0x20) >= 'a' && (c|0x20) <= 'z' }
func isAlnum(c byte) bool { return isDigit(c) || isAlpha(c) }
func isHex(c byte) bool   { return isDigit(c) || ((c|0x20) >= 'a' && (c|0x20) <= 'f') }

// boundary reports whether position i ends a match: end of string or a non-alphanumeric byte.
func boundary(s string, i int) bool { return i >= len(s) || !isAlnum(s[i]) }

func hexRun(s string, i, max int) int {
	j := i
	for j < len(s) && j-i < max && isHex(s[j]) {
		j++
	}
	return j - i
}

// matchUUID: 8-4-4-4-12 hex digits.
func matchUUID(s string, i int) int {
	if len(s)-i < 36 {
		return 0
	}
	p := i
	for g, want := range [5]int{8, 4, 4, 4, 12} {
		if hexRun(s, p, want) != want {
			return 0
		}
		p += want
		if g < 4 {
			if s[p] != '-' {
				return 0
			}
			p++
		}
	}
	if !boundary(s, p) {
		return 0
	}
	return p - i
}

// matchIPv4: four groups of 1-3 digits separated by dots.
func matchIPv4(s string, i int) int {
	p := i
	for g := 0; g < 4; g++ {
		d := 0
		for p < len(s) && d < 3 && isDigit(s[p]) {
			p++
			d++
		}
		if d == 0 {
			return 0
		}
		if g < 3 {
			if p >= len(s) || s[p] != '.' {
				return 0
			}
			p++
		}
	}
	if !boundary(s, p) {
		return 0
	}
	return p - i
}

// matchHex: 0x-prefixed hex, or a run of 12+ hex characters.
func matchHex(s string, i int) int {
	if s[i] == '0' && i+2 < len(s) && (s[i+1]|0x20) == 'x' {
		if l := hexRun(s, i+2, len(s)); l > 0 && boundary(s, i+2+l) {
			return 2 + l
		}
	}
	if l := hexRun(s, i, len(s)); l >= 12 && boundary(s, i+l) {
		return l
	}
	return 0
}

var units = [...]string{"ns", "us", "ms", "s", "KB", "MB", "GB", "B", "%"}

// matchNum: digits, optional .digits, optional unit (ns us ms s KB MB GB B %).
func matchNum(s string, i int) int {
	p := i
	for p < len(s) && isDigit(s[p]) {
		p++
	}
	if p == i {
		return 0
	}
	if p+1 < len(s) && s[p] == '.' && isDigit(s[p+1]) {
		p++
		for p < len(s) && isDigit(s[p]) {
			p++
		}
	}
	for _, u := range units {
		if len(s)-p >= len(u) && s[p:p+len(u)] == u && boundary(s, p+len(u)) {
			return p + len(u) - i
		}
	}
	if boundary(s, p) {
		return p - i
	}
	return 0
}

func isEmailChar(c byte) bool {
	return isAlnum(c) || c == '.' || c == '_' || c == '%' || c == '+' || c == '-'
}

// findEmail returns the span of the first local@domain.tld in tok.
func findEmail(tok string) (lo, hi int, ok bool) {
	for at := 0; at < len(tok); at++ {
		if tok[at] != '@' {
			continue
		}
		lo = at
		for lo > 0 && isEmailChar(tok[lo-1]) {
			lo--
		}
		hi = at + 1
		dot := -1
		for hi < len(tok) && isEmailChar(tok[hi]) {
			if tok[hi] == '.' {
				dot = hi
			}
			hi++
		}
		for hi > at+1 && (tok[hi-1] == '.' || tok[hi-1] == '-') { // trailing punctuation
			hi--
		}
		if lo < at && dot > at+1 && dot < hi-1 {
			return lo, hi, true
		}
	}
	return 0, 0, false
}
