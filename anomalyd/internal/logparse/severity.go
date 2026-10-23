package logparse

import (
	"slices"
	"strings"
)

// InferLevel guesses "error" or "warn" from keywords anywhere in a masked template, for lines
// that carry no level (or info); "" when nothing matches. It looks at whole words (letters,
// digits, '_'), case-insensitively unless noted:
//
//	error  panic, panicked, fatal, segfault, sigsegv, sigabrt, sigbus, deadlock, oom, oomkilled,
//	       crashloopbackoff, traceback, OutOfMemoryError, StackOverflowError, MemoryError,
//	       RecursionError; "out of memory", "segmentation fault", "core dumped",
//	       "nil pointer dereference", "uncaught exception", "unhandled exception",
//	       "exception in thread", "no space left on device", "too many open files";
//	       upper-case ERROR, CRITICAL, CRIT, SEVERE, EMERG
//	warn   class names ending in Exception or Error (NullPointerException, KeyError: upper-case
//	       first letter, case-sensitive suffix); econnrefused, econnreset; "connection refused",
//	       "connection reset", "broken pipe"; upper-case WARN, WARNING
//
// Not keywords: plain "error"/"Error" ("no error", "Error rate"), "errors=0", "error_count";
// a word used as a key ("fatal=0"); "non-fatal"; words inside a path or URL (/api/panic).
// Drain parameters (<*>) and masks (<NUM>) are skipped and break phrases.
func InferLevel(template string) string {
	best := 0          // 1 warn, 2 error
	var words []string // lower case; "" breaks a phrase
	for _, tok := range strings.Fields(template) {
		if strings.IndexByte(tok, '/') >= 0 {
			words = append(words, "")
			continue
		}
		for i := 0; i < len(tok); {
			if tok[i] == '<' {
				if j := maskEnd(tok, i); j > 0 {
					words = append(words, "")
					i = j
					continue
				}
			}
			if !isWord(tok[i]) {
				i++
				continue
			}
			j := i
			for j < len(tok) && isWord(tok[j]) {
				j++
			}
			w, lw := tok[i:j], strings.ToLower(tok[i:j])
			key := j < len(tok) && tok[j] == '='
			neg := i >= 4 && strings.EqualFold(tok[i-4:i], "non-")
			if !key && !neg {
				best = max(best, wordLevel(w, lw))
			}
			words = append(words, lw)
			i = j
		}
	}
	for _, p := range phrases {
		if best < p.level && hasRun(words, p.words) {
			best = p.level
		}
	}
	return [...]string{"", "warn", "error"}[best]
}

var keywords = map[string]int{
	"panic": 2, "panicked": 2, "fatal": 2, "segfault": 2, "sigsegv": 2, "sigabrt": 2, "sigbus": 2,
	"deadlock": 2, "oom": 2, "oomkilled": 2, "crashloopbackoff": 2, "traceback": 2,
	"outofmemoryerror": 2, "stackoverflowerror": 2, "memoryerror": 2, "recursionerror": 2,
	"econnrefused": 1, "econnreset": 1,
}

var upperWords = map[string]int{"ERROR": 2, "CRITICAL": 2, "CRIT": 2, "SEVERE": 2, "EMERG": 2, "WARN": 1, "WARNING": 1}

var phrases = []struct {
	words []string
	level int
}{
	{[]string{"out", "of", "memory"}, 2},
	{[]string{"segmentation", "fault"}, 2},
	{[]string{"core", "dumped"}, 2},
	{[]string{"nil", "pointer", "dereference"}, 2},
	{[]string{"uncaught", "exception"}, 2},
	{[]string{"unhandled", "exception"}, 2},
	{[]string{"exception", "in", "thread"}, 2},
	{[]string{"no", "space", "left", "on", "device"}, 2},
	{[]string{"too", "many", "open", "files"}, 2},
	{[]string{"connection", "refused"}, 1},
	{[]string{"connection", "reset"}, 1},
	{[]string{"broken", "pipe"}, 1},
}

func wordLevel(w, lw string) int {
	if l, ok := keywords[lw]; ok {
		return l
	}
	if l, ok := upperWords[w]; ok {
		return l
	}
	if w[0] >= 'A' && w[0] <= 'Z' && (len(w) > len("Exception") && strings.HasSuffix(w, "Exception") ||
		len(w) > len("Error") && strings.HasSuffix(w, "Error")) {
		return 1
	}
	return 0
}

// maskEnd returns the index after a Drain mask or parameter (<NUM>, <*>) at s[i], or 0.
func maskEnd(s string, i int) int {
	j := i + 1
	for j < len(s) && (s[j] >= 'A' && s[j] <= 'Z' || s[j] == '*') {
		j++
	}
	if j > i+1 && j < len(s) && s[j] == '>' {
		return j + 1
	}
	return 0
}

func isWord(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

func hasRun(words, run []string) bool {
	for i := 0; i+len(run) <= len(words); i++ {
		if slices.Equal(words[i:i+len(run)], run) {
			return true
		}
	}
	return false
}
