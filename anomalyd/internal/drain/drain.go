package drain

import (
	"strings"
	"sync"
)

// Config mirrors the Drain3 options of the same names.
type Config struct {
	Depth       int     // parse-tree depth, >= 3; Drain3 default 4 (one prefix-token level)
	SimTh       float64 // similarity threshold; Drain3 default 0.4
	MaxChildren int     // max children per inner node; Drain3 default 100
	MaxClusters int     // LRU cap on templates per miner; 0 = unlimited
}

func DefaultConfig() Config { return Config{Depth: 4, SimTh: 0.4, MaxChildren: 100, MaxClusters: 2000} }

// Cluster is one log template.
type Cluster struct {
	ID     int
	Tokens []string // template; variable positions hold Param
	Size   int64    // lines (or weighted counts) matched

	leaf       *node
	prev, next *Cluster // LRU list, most recent at head
}

func (c *Cluster) Template() string { return strings.Join(c.Tokens, " ") }

type node struct {
	children map[string]*node
	clusters []*Cluster
}

type Change uint8

const (
	None Change = iota
	Created
	Updated
)

// Miner is a single Drain instance. It is not safe for concurrent use; see Pool.
type Miner struct {
	cfg          Config
	maxNodeDepth int
	byLen        map[int]*node
	clusters     map[int]*Cluster
	nextID       int
	head, tail   *Cluster
	tok          *Tokenizer
	evicted      int64
}

func NewMiner(cfg Config) *Miner {
	if cfg.Depth < 3 {
		cfg.Depth = 3
	}
	if cfg.MaxChildren < 2 {
		cfg.MaxChildren = 2
	}
	return &Miner{cfg: cfg, maxNodeDepth: cfg.Depth - 2, byLen: map[int]*node{},
		clusters: map[int]*Cluster{}, tok: NewTokenizer()}
}

func (m *Miner) Len() int            { return len(m.clusters) }
func (m *Miner) Evicted() int64      { return m.evicted }
func (m *Miner) Get(id int) *Cluster { return m.clusters[id] }

// Clusters calls fn for every template, most recently used first.
func (m *Miner) Clusters(fn func(*Cluster)) {
	for c := m.head; c != nil; c = c.next {
		fn(c)
	}
}

// AddLine masks and tokenizes a raw log message, then adds it.
func (m *Miner) AddLine(line string) (*Cluster, Change) { return m.Add(m.tok.Tokenize(line), 1) }

// Add adds a pre-tokenized message (or a template from another miner) with weight n.
func (m *Miner) Add(toks []string, n int64) (*Cluster, Change) {
	c := m.search(toks)
	if c == nil {
		c = &Cluster{ID: m.nextID + 1, Tokens: cloneTokens(toks), Size: n}
		m.nextID++
		m.clusters[c.ID] = c
		m.insert(c)
		m.touch(c)
		m.evict()
		return c, Created
	}
	ch := None
	for i, t := range c.Tokens {
		if t != Param && t != toks[i] {
			c.Tokens[i] = Param
			ch = Updated
		}
	}
	c.Size += n
	m.touch(c)
	return c, ch
}

// Hit counts n more lines for an existing template and marks it recently used.
func (m *Miner) Hit(id int, n int64) *Cluster {
	c := m.clusters[id]
	if c != nil {
		c.Size += n
		m.touch(c)
	}
	return c
}

// Restore re-inserts a template with a known id (used when loading a snapshot).
func (m *Miner) Restore(id int, toks []string, size int64) {
	c := &Cluster{ID: id, Tokens: cloneTokens(toks), Size: size}
	m.clusters[id] = c
	m.insert(c)
	m.touch(c)
	if id > m.nextID {
		m.nextID = id
	}
}

func cloneTokens(toks []string) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = strings.Clone(t) // don't pin the caller's line buffer
	}
	return out
}

// search follows Drain3 tree_search + fast_match (include_params=false).
func (m *Miner) search(toks []string) *Cluster {
	cur := m.byLen[len(toks)]
	if cur == nil {
		return nil
	}
	depth := 1
	for _, t := range toks {
		if depth >= m.maxNodeDepth || depth == len(toks) {
			break
		}
		next := cur.children[t]
		if next == nil {
			next = cur.children[Param]
		}
		if next == nil {
			return nil
		}
		cur = next
		depth++
	}
	var best *Cluster
	bestSim, bestParams := -1.0, -1
	for _, c := range cur.clusters {
		sim, params := seqDistance(c.Tokens, toks)
		if sim > bestSim || (sim == bestSim && params > bestParams) {
			best, bestSim, bestParams = c, sim, params
		}
	}
	if best != nil && bestSim >= m.cfg.SimTh {
		return best
	}
	return nil
}

func seqDistance(tpl, toks []string) (float64, int) {
	if len(tpl) == 0 {
		return 1, 0
	}
	same, params := 0, 0
	for i, t := range tpl {
		if t == Param {
			params++
		} else if t == toks[i] {
			same++
		}
	}
	return float64(same) / float64(len(tpl)), params
}

// insert follows Drain3 add_seq_to_prefix_tree, including the numeric-token and
// max_children rules.
func (m *Miner) insert(c *Cluster) {
	n := len(c.Tokens)
	cur := m.byLen[n]
	if cur == nil {
		cur = &node{children: map[string]*node{}}
		m.byLen[n] = cur
	}
	if n == 0 {
		cur.clusters = append(cur.clusters, c)
		c.leaf = cur
		return
	}
	depth := 1
	for _, t := range c.Tokens {
		if depth >= m.maxNodeDepth || depth >= n {
			break
		}
		if next, ok := cur.children[t]; ok {
			cur = next
		} else {
			cur = m.child(cur, t)
		}
		depth++
	}
	cur.clusters = append(cur.clusters, c)
	c.leaf = cur
}

func (m *Miner) child(cur *node, t string) *node {
	add := func(key string) *node {
		nn := &node{children: map[string]*node{}}
		cur.children[strings.Clone(key)] = nn
		return nn
	}
	if hasDigit(t) {
		if w, ok := cur.children[Param]; ok {
			return w
		}
		return add(Param)
	}
	if w, ok := cur.children[Param]; ok {
		if len(cur.children) < m.cfg.MaxChildren {
			return add(t)
		}
		return w
	}
	switch k := len(cur.children) + 1; {
	case k < m.cfg.MaxChildren:
		return add(t)
	case k == m.cfg.MaxChildren:
		return add(Param)
	default: // a full node normally already has a Param child
		if w := cur.children[Param]; w != nil {
			return w
		}
		return add(Param)
	}
}

func hasDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if isDigit(s[i]) {
			return true
		}
	}
	return false
}

func (m *Miner) touch(c *Cluster) {
	if m.head == c {
		return
	}
	m.unlink(c)
	c.next = m.head
	if m.head != nil {
		m.head.prev = c
	}
	m.head = c
	if m.tail == nil {
		m.tail = c
	}
}

func (m *Miner) unlink(c *Cluster) {
	if c.prev != nil {
		c.prev.next = c.next
	}
	if c.next != nil {
		c.next.prev = c.prev
	}
	if m.head == c {
		m.head = c.next
	}
	if m.tail == c {
		m.tail = c.prev
	}
	c.prev, c.next = nil, nil
}

func (m *Miner) evict() {
	for m.cfg.MaxClusters > 0 && len(m.clusters) > m.cfg.MaxClusters {
		c := m.tail
		m.unlink(c)
		delete(m.clusters, c.ID)
		leaf := c.leaf
		for i, x := range leaf.clusters {
			if x == c {
				leaf.clusters = append(leaf.clusters[:i], leaf.clusters[i+1:]...)
				break
			}
		}
		m.evicted++
	}
}

// Pool holds one Miner per key (service), each behind its own mutex, with a cap on keys.
type Pool struct {
	cfg     Config
	maxKeys int
	mu      sync.RWMutex
	miners  map[string]*Locked
}

type Locked struct {
	sync.Mutex
	*Miner
}

const OverflowKey = "_other"

func NewPool(cfg Config, maxKeys int) *Pool {
	return &Pool{cfg: cfg, maxKeys: maxKeys, miners: map[string]*Locked{}}
}

// Get returns the miner for key, creating it; keys beyond maxKeys share OverflowKey.
// The returned key is the one actually used.
func (p *Pool) Get(key string) (*Locked, string) {
	p.mu.RLock()
	m := p.miners[key]
	p.mu.RUnlock()
	if m != nil {
		return m, key
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if m = p.miners[key]; m != nil {
		return m, key
	}
	if p.maxKeys > 0 && len(p.miners) >= p.maxKeys {
		key = OverflowKey
		if m = p.miners[key]; m != nil {
			return m, key
		}
	}
	m = &Locked{Miner: NewMiner(p.cfg)}
	p.miners[strings.Clone(key)] = m
	return m, key
}

// Keys maps each miner to its key.
func (p *Pool) Keys() map[*Locked]string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[*Locked]string, len(p.miners))
	for k, m := range p.miners {
		out[m] = k
	}
	return out
}

// Each calls fn for every key with the miner locked.
func (p *Pool) Each(fn func(key string, m *Miner)) {
	p.mu.RLock()
	keys := make([]string, 0, len(p.miners))
	for k := range p.miners {
		keys = append(keys, k)
	}
	p.mu.RUnlock()
	for _, k := range keys {
		p.mu.RLock()
		l := p.miners[k]
		p.mu.RUnlock()
		l.Lock()
		fn(k, l.Miner)
		l.Unlock()
	}
}
