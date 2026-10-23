package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"runtime"
	"strings"
	"testing"
)

func TestLogFlags(t *testing.T) {
	def := slog.Default()
	t.Cleanup(func() { slog.SetDefault(def) })

	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	n := fs.Int("for", 2, "")
	if err := parseFlags(fs, []string{"--log-format=json", "--log-level", "warn", "--for", "3"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, ok := slog.Default().Handler().(*slog.JSONHandler); !ok || *n != 3 ||
		slog.Default().Enabled(ctx, slog.LevelInfo) || !slog.Default().Enabled(ctx, slog.LevelWarn) {
		t.Fatalf("handler %T, info enabled %v", slog.Default().Handler(), slog.Default().Enabled(ctx, slog.LevelInfo))
	}

	var b bytes.Buffer
	if err := setLogger(&b, "json", "debug"); err != nil {
		t.Fatal(err)
	}
	slog.Debug("hello", "k", 1)
	var rec map[string]any
	if err := json.Unmarshal(b.Bytes(), &rec); err != nil || rec["msg"] != "hello" || rec["level"] != "DEBUG" {
		t.Fatalf("json log line %q: %v", b.String(), err)
	}
	for _, bad := range [][2]string{{"xml", "info"}, {"text", "loud"}} {
		if err := setLogger(&b, bad[0], bad[1]); err == nil {
			t.Errorf("setLogger(%q, %q) accepted", bad[0], bad[1])
		}
	}
}

func TestVersionString(t *testing.T) {
	defer func(v string) { version = v }(version)
	version = "v9.9.9" // as set by -ldflags "-X main.version=v9.9.9"
	if v := versionString(); !strings.HasPrefix(v, "anomalyd v9.9.9 (revision ") || !strings.Contains(v, runtime.Version()) {
		t.Fatalf("version = %q", v)
	}
}
