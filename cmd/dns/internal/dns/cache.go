package dns

import (
	"container/list"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// cacheKey identifies a cached response.
type cacheKey struct {
	name  string
	qtype uint16
}

type cacheEntry struct {
	key     cacheKey
	msg     *dns.Msg
	expires time.Time
}

// dnsCache is a TTL-aware bounded LRU cache for upstream DNS responses.
type dnsCache struct {
	mu      sync.Mutex
	maxSize int
	now     func() time.Time
	items   map[cacheKey]*list.Element
	lru     *list.List
}

func newDNSCache(maxSize int) *dnsCache {
	if maxSize <= 0 {
		maxSize = 1
	}

	return &dnsCache{
		maxSize: maxSize,
		now:     time.Now,
		items:   make(map[cacheKey]*list.Element, maxSize),
		lru:     list.New(),
	}
}

// Get returns a copy of the cached message with TTL adjusted to remaining time,
// or nil if not present / expired.
func (c *dnsCache) Get(name string, qtype uint16) *dns.Msg {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey{name: name, qtype: qtype}

	element, ok := c.items[key]
	if !ok {
		return nil
	}

	entry, _ := element.Value.(*cacheEntry)
	if c.now().After(entry.expires) {
		c.lru.Remove(element)
		delete(c.items, key)

		return nil
	}

	c.lru.MoveToFront(element)
	remaining := uint32(entry.expires.Sub(c.now()).Seconds())

	out := entry.msg.Copy()
	for _, rr := range out.Answer {
		rr.Header().Ttl = remaining
	}

	for _, rr := range out.Ns {
		rr.Header().Ttl = remaining
	}

	for _, rr := range out.Extra {
		rr.Header().Ttl = remaining
	}

	return out
}

// Put stores a response. ttl is the lifetime in seconds; 0 = do not cache.
func (c *dnsCache) Put(name string, qtype uint16, msg *dns.Msg, ttl uint32) {
	if msg == nil || ttl == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey{name: name, qtype: qtype}

	if existing, ok := c.items[key]; ok {
		entry, _ := existing.Value.(*cacheEntry)
		entry.msg = msg.Copy()
		entry.expires = c.now().Add(time.Duration(ttl) * time.Second)

		c.lru.MoveToFront(existing)

		return
	}

	entry := &cacheEntry{
		key:     key,
		msg:     msg.Copy(),
		expires: c.now().Add(time.Duration(ttl) * time.Second),
	}
	element := c.lru.PushFront(entry)
	c.items[key] = element

	if c.lru.Len() > c.maxSize {
		c.evictOldest()
	}
}

// Len returns the current number of entries (for tests/metrics).
func (c *dnsCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.lru.Len()
}

func (c *dnsCache) evictOldest() {
	element := c.lru.Back()
	if element == nil {
		return
	}

	entry, _ := element.Value.(*cacheEntry)
	c.lru.Remove(element)
	delete(c.items, entry.key)
}

// minAnswerTTL returns the minimum TTL across answer/ns/extra. Returns 0 if no
// records are present.
func minAnswerTTL(msg *dns.Msg) uint32 {
	const maxTTL uint32 = 1<<32 - 1

	minTTL := maxTTL

	scan := func(rrs []dns.RR) {
		for _, rr := range rrs {
			ttl := rr.Header().Ttl
			if ttl < minTTL {
				minTTL = ttl
			}
		}
	}

	scan(msg.Answer)
	scan(msg.Ns)
	scan(msg.Extra)

	if minTTL == maxTTL {
		return 0
	}

	return minTTL
}
