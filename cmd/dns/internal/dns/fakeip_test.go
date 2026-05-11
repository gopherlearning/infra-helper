package dns_test

import (
	"net"
	"net/netip"
	"strconv"
	"testing"

	dnssvc "infra.helper/cmd/dns/internal/dns"
)

func TestFakeIPDeterministic(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "198.18.0.0/15", "fc00::/7")

	first := pool.ForA("instagram.com")
	second := pool.ForA("instagram.com")

	if first != second {
		t.Errorf("expected deterministic IP, got %s vs %s", first, second)
	}

	other := pool.ForA("twitter.com")
	if other == first {
		t.Errorf("different domains hashed to the same IP: %s", first)
	}

	v4Net := mustCIDR(t, "198.18.0.0/15")

	addr, parseErr := netip.ParseAddr(first)
	if parseErr != nil {
		t.Fatalf("parse first: %v", parseErr)
	}

	if !v4Net.Contains(addr.AsSlice()) {
		t.Errorf("ip %s not in range %s", first, v4Net)
	}
}

func TestFakeIPv6(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "198.18.0.0/15", "fc00::/7")

	addrStr := pool.ForAAAA("example.com")

	addr, err := netip.ParseAddr(addrStr)
	if err != nil {
		t.Fatalf("parse v6: %v", err)
	}

	v6Net := mustCIDR(t, "fc00::/7")
	if !v6Net.Contains(addr.AsSlice()) {
		t.Errorf("ip %s not in range %s", addrStr, v6Net)
	}

	if !pool.HasV6() {
		t.Error("HasV6 should be true")
	}
}

func TestFakeIPNoV6(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "198.18.0.0/15", "")
	if pool.HasV6() {
		t.Error("HasV6 should be false when no v6 range configured")
	}
}

func TestFakeIPSkipsNetworkAddress(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "10.0.0.0/24", "")

	const iterations = 1000

	for i := range iterations {
		domain := "host-" + strconv.Itoa(i) + ".local"
		addrStr := pool.ForA(domain)

		addr, err := netip.ParseAddr(addrStr)
		if err != nil {
			t.Fatalf("parse %s: %v", addrStr, err)
		}

		raw := addr.As4()
		if raw[3] == 0 || raw[3] == 255 {
			t.Errorf("got reserved address %s for %s", addrStr, domain)
		}
	}
}

func mustPool(t *testing.T, v4, v6 string) *dnssvc.FakeIPPool {
	t.Helper()

	pool, err := dnssvc.NewFakeIPPoolForTest(v4, v6)
	if err != nil {
		t.Fatalf("NewFakeIPPoolForTest: %v", err)
	}

	return pool
}

func mustCIDR(t *testing.T, cidr string) *net.IPNet {
	t.Helper()

	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("parse cidr %s: %v", cidr, err)
	}

	return ipnet
}
