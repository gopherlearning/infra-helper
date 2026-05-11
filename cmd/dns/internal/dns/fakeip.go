package dns

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"strings"
	"sync"
)

const (
	fakeIPHashBytes        = 16
	reservedIPv4Endpoints  = 2
	minRangeForReservation = 4
)

var (
	errFakeIPNotV4 = errors.New("fakeip ipv4_range is not IPv4")
	errFakeIPNotV6 = errors.New("fakeip ipv6_range is not IPv6")
)

// fakeIPPool deterministically maps domain → IP within the configured range.
type fakeIPPool struct {
	v4Net   *net.IPNet
	v6Net   *net.IPNet
	v4Size  *big.Int
	v6Size  *big.Int
	v4Base  *big.Int
	v6Base  *big.Int
	hasV4   bool
	hasV6   bool
	mu      sync.RWMutex
	v4Cache map[string]netip.Addr
	v6Cache map[string]netip.Addr
}

func newFakeIPPool(v4CIDR, v6CIDR string) (*fakeIPPool, error) {
	pool := &fakeIPPool{
		v4Cache: make(map[string]netip.Addr),
		v6Cache: make(map[string]netip.Addr),
	}

	v4Err := pool.initV4(v4CIDR)
	if v4Err != nil {
		return nil, v4Err
	}

	v6Err := pool.initV6(v6CIDR)
	if v6Err != nil {
		return nil, v6Err
	}

	return pool, nil
}

// HasV4 reports whether IPv4 fakeip is configured.
func (p *fakeIPPool) HasV4() bool { return p != nil && p.hasV4 }

// HasV6 reports whether IPv6 fakeip is configured.
func (p *fakeIPPool) HasV6() bool { return p != nil && p.hasV6 }

// ForA returns the deterministic IPv4 fake address for the FQDN.
func (p *fakeIPPool) ForA(fqdn string) netip.Addr {
	key := normalizeFQDN(fqdn)

	p.mu.RLock()
	addr, ok := p.v4Cache[key]
	p.mu.RUnlock()

	if ok {
		return addr
	}

	addr = p.computeV4(key)

	p.mu.Lock()
	p.v4Cache[key] = addr
	p.mu.Unlock()

	return addr
}

// ForAAAA returns the deterministic IPv6 fake address for the FQDN.
func (p *fakeIPPool) ForAAAA(fqdn string) netip.Addr {
	key := normalizeFQDN(fqdn)

	p.mu.RLock()
	addr, ok := p.v6Cache[key]
	p.mu.RUnlock()

	if ok {
		return addr
	}

	addr = p.computeV6(key)

	p.mu.Lock()
	p.v6Cache[key] = addr
	p.mu.Unlock()

	return addr
}

// IsFakeV4 reports whether the IP belongs to the configured IPv4 fakeip range.
func (p *fakeIPPool) IsFakeV4(addr net.IP) bool {
	if p == nil || !p.hasV4 || addr == nil {
		return false
	}

	asV4 := addr.To4()
	if asV4 == nil {
		return false
	}

	return p.v4Net.Contains(asV4)
}

func (p *fakeIPPool) initV4(cidr string) error {
	if cidr == "" {
		return nil
	}

	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("parse fakeip ipv4_range: %w", err)
	}

	asV4 := ipnet.IP.To4()
	if asV4 == nil {
		return fmt.Errorf("%w: %s", errFakeIPNotV4, cidr)
	}

	ones, bits := ipnet.Mask.Size()
	p.v4Net = ipnet
	p.v4Base = new(big.Int).SetBytes(asV4)
	p.v4Size = new(big.Int).Lsh(big.NewInt(1), safeBits(bits-ones))
	p.hasV4 = true

	return nil
}

func (p *fakeIPPool) initV6(cidr string) error {
	if cidr == "" {
		return nil
	}

	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("parse fakeip ipv6_range: %w", err)
	}

	asV6 := ipnet.IP.To16()
	if asV6 == nil {
		return fmt.Errorf("%w: %s", errFakeIPNotV6, cidr)
	}

	ones, bits := ipnet.Mask.Size()
	p.v6Net = ipnet
	p.v6Base = new(big.Int).SetBytes(asV6)
	p.v6Size = new(big.Int).Lsh(big.NewInt(1), safeBits(bits-ones))
	p.hasV6 = true

	return nil
}

func safeBits(value int) uint {
	if value < 0 {
		return 0
	}

	return uint(value)
}

func (p *fakeIPPool) computeV4(key string) netip.Addr {
	usable := new(big.Int).Set(p.v4Size)

	if usable.Cmp(big.NewInt(minRangeForReservation)) >= 0 {
		usable = new(big.Int).Sub(usable, big.NewInt(reservedIPv4Endpoints))
	}

	offset := hashOffset(key, usable)
	if usable.Cmp(p.v4Size) < 0 {
		offset = new(big.Int).Add(offset, big.NewInt(1))
	}

	ipValue := new(big.Int).Add(p.v4Base, offset)
	buf := ipToFixed(ipValue, net.IPv4len)

	return netip.AddrFrom4([4]byte(buf))
}

func (p *fakeIPPool) computeV6(key string) netip.Addr {
	offset := hashOffset(key, p.v6Size)
	ipValue := new(big.Int).Add(p.v6Base, offset)
	buf := ipToFixed(ipValue, net.IPv6len)

	return netip.AddrFrom16([16]byte(buf))
}

func hashOffset(key string, size *big.Int) *big.Int {
	if size.Sign() <= 0 {
		return big.NewInt(0)
	}

	sum := sha256.Sum256([]byte(key))
	hashInt := new(big.Int).SetBytes(sum[:fakeIPHashBytes])

	return new(big.Int).Mod(hashInt, size)
}

func ipToFixed(value *big.Int, width int) []byte {
	raw := value.Bytes()
	if len(raw) >= width {
		return raw[len(raw)-width:]
	}

	out := make([]byte, width)
	copy(out[width-len(raw):], raw)

	return out
}

func normalizeFQDN(name string) string {
	name = strings.TrimSuffix(name, ".")

	return strings.ToLower(name)
}
