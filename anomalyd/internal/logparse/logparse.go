// Package logparse pulls a few fields out of JSON log lines without decoding the whole object,
// and normalises log levels.
package logparse

import (
	"encoding/json"
	"strings"
)

// Extractor reads string values at dotted paths ("service.name") from one JSON object.
// Both nested objects ({"service":{"name":"x"}}) and flat dotted keys ({"service.name":"x"})
// match. Unescaped strings are returned as substrings of the line (no copy); non-string
// values are returned as raw JSON text. Not safe for concurrent use.
type Extractor struct {
	paths []string
	path  []byte
	out   []string
}

func NewExtractor(paths ...string) *Extractor { return &Extractor{paths: paths} }

// Extract returns one value per path ("" when absent); ok is false when line is not a JSON object.
// The returned slice is reused by the next call.
func (e *Extractor) Extract(line string) (vals []string, ok bool) {
	if cap(e.out) < len(e.paths) {
		e.out = make([]string, len(e.paths))
	}
	e.out = e.out[:len(e.paths)]
	clear(e.out)
	e.path = e.path[:0]
	i := skipWS(line, 0)
	if i >= len(line) || line[i] != '{' {
		return e.out, false
	}
	_, ok = e.object(line, i)
	return e.out, ok
}

// object parses {...} starting at s[i]=='{' with e.path as the current prefix.
func (e *Extractor) object(s string, i int) (int, bool) {
	i++
	base := len(e.path)
	for {
		i = skipWS(s, i)
		if i >= len(s) {
			return i, false
		}
		if s[i] == '}' {
			return i + 1, true
		}
		if s[i] == ',' {
			i++
			continue
		}
		key, j, ok := str(s, i)
		if !ok {
			return j, false
		}
		i = skipWS(s, j)
		if i >= len(s) || s[i] != ':' {
			return i, false
		}
		i = skipWS(s, i+1)
		if i >= len(s) {
			return i, false
		}
		e.path = e.path[:base]
		if base > 0 {
			e.path = append(e.path, '.')
		}
		e.path = append(e.path, key...)
		switch {
		case s[i] == '"':
			v, j, ok := str(s, i)
			if !ok {
				return j, false
			}
			e.set(v)
			i = j
		case s[i] == '{' && e.descend():
			j, ok := e.object(s, i)
			if !ok {
				return j, false
			}
			i = j
		default:
			j, ok := skipValue(s, i)
			if !ok {
				return j, false
			}
			e.set(s[i:j])
			i = j
		}
		e.path = e.path[:base]
	}
}

func (e *Extractor) set(v string) {
	for k, p := range e.paths {
		if p == string(e.path) {
			e.out[k] = v
		}
	}
}

// descend reports whether some wanted path lies below the current one.
func (e *Extractor) descend() bool {
	for _, p := range e.paths {
		if len(p) > len(e.path) && p[len(e.path)] == '.' && p[:len(e.path)] == string(e.path) {
			return true
		}
	}
	return false
}

func skipWS(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

// str parses a JSON string at s[i]=='"'. Escaped strings are decoded (allocates).
func str(s string, i int) (string, int, bool) {
	if i >= len(s) || s[i] != '"' {
		return "", i, false
	}
	esc := false
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			esc = true
			j++
		case '"':
			if !esc {
				return s[i+1 : j], j + 1, true
			}
			var v string
			if json.Unmarshal([]byte(s[i:j+1]), &v) != nil {
				return "", j + 1, false
			}
			return v, j + 1, true
		}
	}
	return "", len(s), false
}

// skipValue skips a number, literal, array or object.
func skipValue(s string, i int) (int, bool) {
	depth := 0
	for i < len(s) {
		switch c := s[i]; c {
		case '"':
			_, j, ok := str(s, i)
			if !ok {
				return j, false
			}
			i = j
			if depth == 0 {
				return i, true
			}
			continue
		case '{', '[':
			depth++
		case '}', ']':
			if depth == 0 {
				return i, true
			}
			depth--
			if depth == 0 {
				return i + 1, true
			}
		case ',':
			if depth == 0 {
				return i, true
			}
		case ' ', '\t', '\n', '\r':
			if depth == 0 {
				return i, true
			}
		}
		i++
	}
	return i, depth == 0
}

// Level normalises a level string to error, warn, info, debug or "" (unknown).
func Level(v string) string {
	if len(v) > len("informational") {
		return ""
	}
	switch strings.ToLower(v) {
	case "error", "err", "e", "fatal", "critical", "crit", "panic", "alert", "emerg", "emergency", "severe":
		return "error"
	case "warn", "warning", "w":
		return "warn"
	case "info", "i", "notice", "information", "informational":
		return "info"
	case "debug", "d", "trace", "t", "fine", "finer", "finest":
		return "debug"
	}
	return ""
}

// DetectLevel finds a level word among the first tokens of a plain-text message
// ("ERROR ...", "[WARN] ...", "level=error ...").
func DetectLevel(msg string) string {
	for n, i := 0, 0; n < 6 && i < len(msg); n++ {
		for i < len(msg) && msg[i] == ' ' {
			i++
		}
		j := i
		for j < len(msg) && msg[j] != ' ' {
			j++
		}
		tok := strings.Trim(msg[i:j], "[]():,")
		if k := strings.IndexByte(tok, '='); k >= 0 {
			tok = strings.Trim(tok[k+1:], `"`)
		}
		if len(tok) >= 4 {
			if l := Level(tok); l != "" {
				return l
			}
		}
		i = j
	}
	return ""
}
