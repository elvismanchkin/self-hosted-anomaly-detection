// Package wire defines the agent → server push format and a tiny Prometheus text writer.
package wire

import (
	"bufio"
	"io"
	"math"
	"strconv"
	"strings"
)

// Push is what an agent POSTs to /api/v1/templates (JSON, optionally gzip-encoded).
// Counts are deltas since the previous push, per bucket; the server adds them up.
type Push struct {
	Agent string `json:"agent"`
	Step  int64  `json:"step"` // seconds per count bucket
	Items []Item `json:"items"`
}

type Item struct {
	Service  string     `json:"service"`
	Template string     `json:"template"` // masked Drain template from the agent's miner
	Level    string     `json:"level,omitempty"`
	Counts   [][2]int64 `json:"counts"` // [bucket start, unix seconds; lines]
}

// Expo writes the Prometheus text exposition format (version 0.0.4).
type Expo struct {
	w *bufio.Writer
}

func NewExpo(w io.Writer) *Expo { return &Expo{w: bufio.NewWriterSize(w, 64<<10)} }

func (e *Expo) Family(name, help, typ string) {
	e.w.WriteString("# HELP " + name + " " + help + "\n# TYPE " + name + " " + typ + "\n")
}

// Sample writes one line. labels are name/value pairs; invalid names are skipped.
func (e *Expo) Sample(name string, labels [][2]string, v float64) {
	e.w.WriteString(name)
	first := true
	for _, l := range labels {
		if !ValidLabelName(l[0]) {
			continue
		}
		if first {
			e.w.WriteByte('{')
			first = false
		} else {
			e.w.WriteByte(',')
		}
		e.w.WriteString(l[0])
		e.w.WriteString(`="`)
		e.w.WriteString(escaper.Replace(l[1]))
		e.w.WriteByte('"')
	}
	if !first {
		e.w.WriteByte('}')
	}
	e.w.WriteByte(' ')
	e.w.WriteString(FormatFloat(v))
	e.w.WriteByte('\n')
}

func (e *Expo) Flush() error { return e.w.Flush() }

var escaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func FormatFloat(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// ValidLabelName: [a-zA-Z_][a-zA-Z0-9_]*, not starting with "__".
func ValidLabelName(s string) bool {
	if s == "" || strings.HasPrefix(s, "__") {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c == '_' || (c|0x20) >= 'a' && (c|0x20) <= 'z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
