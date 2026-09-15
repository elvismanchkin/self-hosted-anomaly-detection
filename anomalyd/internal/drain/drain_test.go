package drain

import (
	"fmt"
	"strings"
	"testing"
)

func TestMask(t *testing.T) {
	cases := map[string]string{
		"took=1606ms":                          "took=<NUM>",
		"latency_ms=123":                       "latency_ms=<NUM>",
		"total=12.50":                          "total=<NUM>",
		"wh-12":                                "wh-<NUM>",
		"SKU-4821":                             "SKU-<NUM>",
		"/api/v1/cart/123":                     "/api/v1/cart/<NUM>",
		"HTTP/1.1\"":                           "HTTP/<NUM>\"",
		"10.1.22.254":                          "<IP>",
		"client=10.0.0.1:8080":                 "client=<IP>:<NUM>",
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8": "<UUID>",
		"session=6ba7b810-9dad-11d1-80b4-00c04fd430c8,": "session=<UUID>,",
		"q_hash=00ff00ff00ff00ff":                       "q_hash=<HEX>",
		"0x1f":                                          "<HEX>",
		"50%":                                           "<NUM>",
		"12%abc":                                        "<NUM>%abc",
		"5MB":                                           "<NUM>",
		"user42":                                        "user42", // digits glued to letters stay (Drain3 regex parity)
		"(active=12":                                    "(active=<NUM>",
		"idle=0)":                                       "idle=<NUM>)",
		"mail=john.doe+x@example.co.uk;":                "mail=<EMAIL>;",
		"@home":                                         "@home",
		"plain":                                         "plain",
	}
	tk := NewTokenizer()
	for in, want := range cases {
		got := tk.Tokenize(in)
		if len(got) != 1 || got[0] != want {
			t.Errorf("mask(%q) = %q, want %q", in, got, want)
		}
	}
	got := strings.Join(tk.Tokenize("  GET /x  200\t12ms "), "|")
	if got != "GET|/x|<NUM>|<NUM>" {
		t.Errorf("tokenize = %q", got)
	}
}

func TestClusters(t *testing.T) {
	m := NewMiner(DefaultConfig())
	lines := []string{
		"Order 1234567 created for user 42 total=10.50 currency=EUR",
		"Order 7654321 created for user 7 total=99.00 currency=EUR",
		"User alice logged in from 10.0.0.1",
		"User bob logged in from 10.0.0.2",
		"Job 6ba7b810-9dad-11d1-80b4-00c04fd430c8 finished in 12ms",
	}
	var ids []int
	var changes []Change
	for _, l := range lines {
		c, ch := m.AddLine(l)
		ids = append(ids, c.ID)
		changes = append(changes, ch)
	}
	if ids[0] != ids[1] || ids[2] != ids[3] || ids[0] == ids[2] || ids[4] == ids[0] {
		t.Fatalf("unexpected grouping %v", ids)
	}
	if changes[0] != Created || changes[1] != None || changes[3] != Updated {
		t.Fatalf("unexpected changes %v", changes)
	}
	if got := m.Get(ids[2]).Template(); got != "User <*> logged in from <IP>" {
		t.Fatalf("template = %q", got)
	}
	if got := m.Get(ids[0]).Template(); got != "Order <NUM> created for user <NUM> total=<NUM> currency=EUR" {
		t.Fatalf("template = %q", got)
	}
	// A template from another miner lands in the same cluster.
	c, ch := m.Add(strings.Fields("User <*> logged in from <IP>"), 5)
	if c.ID != ids[2] || ch != None || c.Size != 7 {
		t.Fatalf("template merge: id=%d change=%v size=%d", c.ID, ch, c.Size)
	}
}

func TestLRU(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxClusters = 3
	m := NewMiner(cfg)
	for i := 0; i < 5; i++ {
		m.AddLine(fmt.Sprintf("distinct%c message kind", 'a'+i))
	}
	if m.Len() != 3 || m.Evicted() != 2 {
		t.Fatalf("len=%d evicted=%d", m.Len(), m.Evicted())
	}
	if c, ch := m.AddLine("distincta message kind"); ch != Created || c.ID != 6 {
		t.Fatalf("evicted template should be re-created, got id=%d change=%v", c.ID, ch)
	}
}

func TestMaxChildren(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Depth = 5 // two prefix levels so the second token fans out
	cfg.MaxChildren = 4
	m := NewMiner(cfg)
	for i := 0; i < 10; i++ {
		m.AddLine(fmt.Sprintf("op %c done", 'a'+i))
	}
	if m.Len() > 10 || m.byLen[3].children["op"] == nil || len(m.byLen[3].children["op"].children) > 4 {
		t.Fatalf("fan-out not capped: %d clusters", m.Len())
	}
}

func BenchmarkTokenize(b *testing.B) {
	tk := NewTokenizer()
	line := `Payment authorized order=1234567 provider=stripe latency_ms=1606 from 10.1.2.3 id=6ba7b810-9dad-11d1-80b4-00c04fd430c8`
	b.SetBytes(int64(len(line)))
	for b.Loop() {
		tk.Tokenize(line)
	}
}

func BenchmarkAddLine(b *testing.B) {
	m := NewMiner(DefaultConfig())
	lines := make([]string, 1024)
	tpls := []string{
		"Order %d created for user %d total=%d.50 currency=EUR",
		"Payment authorized order=%d provider=stripe latency_ms=%d attempt=%d",
		"GET /api/v1/cart/%d 200 %dms %d",
		"Cache miss key=search:%016x hits=%d shard=%d",
	}
	for i := range lines {
		lines[i] = fmt.Sprintf(tpls[i%len(tpls)], i*7919, i%97, i%13)
	}
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		m.AddLine(lines[i&1023])
	}
}
