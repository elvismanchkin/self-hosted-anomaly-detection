package logparse

import "strings"

// Container runtimes write each container's stdout/stderr to a file on the node, one envelope per
// line. These are the files Filebeat reads for "container stdout" (filestream + `container` parser)
// and the OTel `container` operator parses:
//
//	cri (containerd, CRI-O): 2026-09-15T10:00:00.123456789Z stdout F <text>   tag P = partial
//	docker (json-file):      {"log":"<text>\n","stream":"stdout","time":"..."}  no trailing \n = partial
//
// Runtimes split long lines into chunks (16 KiB), so callers join partial chunks per stream.

// Envelope is one unwrapped container log line.
type Envelope struct {
	Text    string
	Stderr  bool
	Partial bool
}

// Unwrapper parses container log envelopes. Not safe for concurrent use.
type Unwrapper struct {
	format string // cri | docker | auto
	ex     *Extractor
}

func NewUnwrapper(format string) *Unwrapper {
	return &Unwrapper{format: format, ex: NewExtractor("log", "stream")}
}

// Unwrap returns ok=false when line is not a container envelope ("auto": neither format).
func (u *Unwrapper) Unwrap(line string) (Envelope, bool) {
	switch u.format {
	case "cri":
		return cri(line)
	case "docker":
		return u.docker(line)
	}
	if strings.HasPrefix(line, "{") {
		return u.docker(line)
	}
	return cri(line)
}

func cri(line string) (Envelope, bool) {
	i := strings.IndexByte(line, ' ')
	if i <= 0 || line[0] < '0' || line[0] > '9' { // RFC 3339 timestamp first
		return Envelope{}, false
	}
	var e Envelope
	rest := line[i+1:]
	switch {
	case strings.HasPrefix(rest, "stdout "):
		rest = rest[7:]
	case strings.HasPrefix(rest, "stderr "):
		rest, e.Stderr = rest[7:], true
	default:
		return Envelope{}, false
	}
	tag := rest
	if j := strings.IndexByte(rest, ' '); j >= 0 {
		tag, e.Text = rest[:j], rest[j+1:]
	}
	// Tags are ':'-separated; the first one is P (partial) or F (full).
	if tag == "" || tag[0] != 'P' && tag[0] != 'F' {
		return Envelope{}, false
	}
	e.Partial = tag[0] == 'P'
	return e, true
}

func (u *Unwrapper) docker(line string) (Envelope, bool) {
	vals, ok := u.ex.Extract(line)
	if !ok || vals[1] != "stdout" && vals[1] != "stderr" { // an app's own JSON line, not an envelope
		return Envelope{}, false
	}
	e := Envelope{Text: vals[0], Stderr: vals[1] == "stderr"}
	if t, full := strings.CutSuffix(e.Text, "\n"); full {
		e.Text = t
	} else {
		e.Partial = true
	}
	return e, true
}
