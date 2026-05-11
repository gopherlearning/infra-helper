package dns_test

import (
	"testing"
	"time"

	"github.com/miekg/dns"
	dnssvc "infra.helper/cmd/dns/internal/dns"
)

func TestCachePutGet(t *testing.T) {
	t.Parallel()

	cache := dnssvc.NewDNSCacheForTest(8)
	cache.Put("example.com", dns.TypeA, 60)

	if !cache.Get("example.com", dns.TypeA) {
		t.Fatal("expected hit")
	}
}

func TestCacheTTLExpiry(t *testing.T) {
	t.Parallel()

	cache := dnssvc.NewDNSCacheForTest(8)
	base := time.Now()

	cache.SetClock(func() time.Time { return base })
	cache.Put("example.com", dns.TypeA, 5)
	cache.SetClock(func() time.Time { return base.Add(10 * time.Second) })

	if cache.Get("example.com", dns.TypeA) {
		t.Error("expected expired entry to miss")
	}
}

func TestCacheLRUEviction(t *testing.T) {
	t.Parallel()

	cache := dnssvc.NewDNSCacheForTest(2)
	cache.Put("a.com", dns.TypeA, 60)
	cache.Put("b.com", dns.TypeA, 60)
	cache.Put("c.com", dns.TypeA, 60)

	if cache.Len() != 2 {
		t.Errorf("expected len 2, got %d", cache.Len())
	}

	if cache.Get("a.com", dns.TypeA) {
		t.Error("a.com should have been evicted")
	}

	if !cache.Get("c.com", dns.TypeA) {
		t.Error("c.com should be present")
	}
}

func TestCacheZeroTTLNotStored(t *testing.T) {
	t.Parallel()

	cache := dnssvc.NewDNSCacheForTest(8)
	cache.Put("example.com", dns.TypeA, 0)

	if cache.Get("example.com", dns.TypeA) {
		t.Error("ttl=0 should not be stored")
	}
}

func TestMinAnswerTTL(t *testing.T) {
	t.Parallel()

	got := dnssvc.MinAnswerTTLForTest([]uint32{600, 30, 120})
	if got != 30 {
		t.Errorf("expected 30, got %d", got)
	}
}
