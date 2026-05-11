package dns

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const (
	defaultUpstreamTimeout = 5 * time.Second
	dohContentType         = "application/dns-message"
	dohIdleConnTimeout     = 90 * time.Second
)

var (
	errAllUpstreamsFailed = errors.New("all upstreams failed")
	errUnknownScheme      = errors.New("unknown upstream scheme")
	errNoUpstreams        = errors.New("no upstreams configured")
)

// upstream is a single resolver target.
type upstream interface {
	Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error)
	Address() string
}

// upstreamPool fans queries out across all configured resolvers.
type upstreamPool struct {
	upstreams []upstream
}

func newUpstreamPool(entries []UpstreamEntry) (*upstreamPool, error) {
	if len(entries) == 0 {
		return nil, errNoUpstreams
	}

	pool := &upstreamPool{upstreams: make([]upstream, 0, len(entries))}

	for _, entry := range entries {
		client, err := buildUpstream(entry)
		if err != nil {
			return nil, fmt.Errorf("upstream %q: %w", entry.Address, err)
		}

		pool.upstreams = append(pool.upstreams, client)
	}

	return pool, nil
}

//nolint:ireturn // returning interface is the whole point of this factory.
func buildUpstream(entry UpstreamEntry) (upstream, error) {
	timeout := entry.Timeout
	if timeout <= 0 {
		timeout = defaultUpstreamTimeout
	}

	parsed, err := url.Parse(entry.Address)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	switch parsed.Scheme {
	case "udp":
		return newDNSClient(parsed.Host, "udp", timeout, entry.Address), nil
	case "tcp":
		return newDNSClient(parsed.Host, "tcp", timeout, entry.Address), nil
	case "tls":
		host := parsed.Host
		if !strings.Contains(host, ":") {
			host += ":853"
		}

		return newDNSClient(host, "tcp-tls", timeout, entry.Address), nil
	case "https", "http":
		return newDoHClient(entry.Address, timeout), nil
	default:
		return nil, fmt.Errorf("%w: %s", errUnknownScheme, parsed.Scheme)
	}
}

type upstreamResult struct {
	msg *dns.Msg
	err error
	src string
}

// Exchange races all upstreams and returns the first successful response.
// SERVFAIL responses are treated as failures.
func (p *upstreamPool) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	resCh := make(chan upstreamResult, len(p.upstreams))

	var wgroup sync.WaitGroup

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, client := range p.upstreams {
		wgroup.Add(1)

		go func(client upstream) {
			defer wgroup.Done()

			resp, err := client.Exchange(subCtx, msg)
			resCh <- upstreamResult{msg: resp, err: err, src: client.Address()}
		}(client)
	}

	go func() {
		wgroup.Wait()
		close(resCh)
	}()

	var lastErr error

	for result := range resCh {
		if result.err == nil && result.msg != nil && result.msg.Rcode != dns.RcodeServerFailure {
			cancel()
			drainResults(resCh)

			return result.msg, nil
		}

		if result.err != nil {
			lastErr = result.err
		}
	}

	if lastErr == nil {
		lastErr = errAllUpstreamsFailed
	}

	return nil, fmt.Errorf("%w: %w", errAllUpstreamsFailed, lastErr)
}

func drainResults(ch <-chan upstreamResult) {
	for range ch { //nolint:revive // intentional drain.
	}
}

// dnsClient handles UDP, TCP, DoT.
type dnsClient struct {
	address string
	display string
	client  *dns.Client
	timeout time.Duration
}

func newDNSClient(host, network string, timeout time.Duration, display string) *dnsClient {
	if !strings.Contains(host, ":") {
		switch network {
		case "tcp-tls":
			host += ":853"
		default:
			host += ":53"
		}
	}

	return &dnsClient{
		address: host,
		display: display,
		client:  &dns.Client{Net: network, Timeout: timeout},
		timeout: timeout,
	}
}

func (c *dnsClient) Address() string { return c.display }

func (c *dnsClient) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	subCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, _, err := c.client.ExchangeContext(subCtx, msg, c.address)
	if err != nil {
		return nil, fmt.Errorf("exchange %s: %w", c.display, err)
	}

	return resp, nil
}

// dohClient handles DNS-over-HTTPS via POST application/dns-message.
type dohClient struct {
	url     string
	client  *http.Client
	timeout time.Duration
}

func newDoHClient(rawURL string, timeout time.Duration) *dohClient {
	return &dohClient{
		url: rawURL,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext:       (&net.Dialer{Timeout: timeout}).DialContext,
				IdleConnTimeout:   dohIdleConnTimeout,
				ForceAttemptHTTP2: true,
			},
		},
		timeout: timeout,
	}
}

func (c *dohClient) Address() string { return c.url }

func (c *dohClient) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	wire, err := msg.Pack()
	if err != nil {
		return nil, fmt.Errorf("pack: %w", err)
	}

	subCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(subCtx, http.MethodPost, c.url, bytes.NewReader(wire))
	if err != nil {
		return nil, fmt.Errorf("doh request: %w", err)
	}

	req.Header.Set("Content-Type", dohContentType)
	req.Header.Set("Accept", dohContentType)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("doh do: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s", errUnexpectedHTTPStatus, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("doh read: %w", err)
	}

	out := new(dns.Msg)

	unpackErr := out.Unpack(body)
	if unpackErr != nil {
		return nil, fmt.Errorf("doh unpack: %w", unpackErr)
	}

	return out, nil
}
