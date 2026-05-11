package dns

// Test-only exports for internal helpers.

import "time"

// FakeIPPool is a test-only wrapper exposing the internal fakeIPPool.
type FakeIPPool struct {
	inner *fakeIPPool
}

// NewFakeIPPoolForTest exposes newFakeIPPool to external tests.
func NewFakeIPPoolForTest(v4CIDR, v6CIDR string) (*FakeIPPool, error) {
	pool, err := newFakeIPPool(v4CIDR, v6CIDR)
	if err != nil {
		return nil, err
	}

	return &FakeIPPool{inner: pool}, nil
}

// ForA delegates to the inner pool.
func (p *FakeIPPool) ForA(name string) string { return p.inner.ForA(name).String() }

// ForAAAA delegates to the inner pool.
func (p *FakeIPPool) ForAAAA(name string) string { return p.inner.ForAAAA(name).String() }

// HasV4 reports whether IPv4 fakeip is configured.
func (p *FakeIPPool) HasV4() bool { return p.inner.HasV4() }

// HasV6 reports whether IPv6 fakeip is configured.
func (p *FakeIPPool) HasV6() bool { return p.inner.HasV6() }

// DNSCacheForTest exposes a TTL-aware DNS cache for tests.
type DNSCacheForTest struct {
	inner *dnsCache
}

// NewDNSCacheForTest constructs a test cache with a controllable clock.
func NewDNSCacheForTest(maxSize int) *DNSCacheForTest {
	return &DNSCacheForTest{inner: newDNSCache(maxSize)}
}

// SetClock overrides the clock used by the cache.
func (c *DNSCacheForTest) SetClock(now func() time.Time) { c.inner.now = now }

// Get returns true if the entry is present and not expired.
func (c *DNSCacheForTest) Get(name string, qtype uint16) bool {
	return c.inner.Get(name, qtype) != nil
}

// Put stores a record with given ttl.
func (c *DNSCacheForTest) Put(name string, qtype uint16, ttl uint32) {
	msg := buildMinimalAMsg(name, ttl)
	c.inner.Put(name, qtype, msg, ttl)
}

// Len returns the cache size.
func (c *DNSCacheForTest) Len() int { return c.inner.Len() }

// MinAnswerTTLForTest exposes minAnswerTTL.
func MinAnswerTTLForTest(ttls []uint32) uint32 {
	return minAnswerTTL(buildMsgWithTTLs(ttls))
}
