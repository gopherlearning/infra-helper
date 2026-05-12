package dns

import (
	"context"
	"crypto/sha1" //nolint:gosec // sha1 used as filename hash, not for security.
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	routercommon "github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"golang.org/x/net/proxy"
	"google.golang.org/protobuf/proto"
)

const (
	ruleFilePerm   os.FileMode = 0o644
	ruleDirPerm    os.FileMode = 0o750
	rulesetUA                  = "infra-helper/dns"
	rulesetTimeout             = 60 * time.Second
)

var (
	errUnexpectedHTTPStatus = errors.New("unexpected HTTP status")
	errUnsupportedProxy     = errors.New("unsupported proxy scheme")
)

// rulesetManager keeps the active matcher and refreshes rulesets in the background.
type rulesetManager struct {
	cfg      Config
	cacheDir string

	clientMu sync.Mutex
	clients  map[string]*http.Client

	mu      sync.RWMutex
	matcher *Matcher

	reloadCh chan struct{}
}

func newRulesetManager(cfg Config) (*rulesetManager, error) {
	mkdirErr := os.MkdirAll(cfg.CacheDir, ruleDirPerm)
	if mkdirErr != nil {
		return nil, fmt.Errorf("mkdir cache: %w", mkdirErr)
	}

	manager := &rulesetManager{
		cfg:      cfg,
		cacheDir: cfg.CacheDir,
		clients:  make(map[string]*http.Client),
		matcher:  NewMatcher(),
		reloadCh: make(chan struct{}, 1),
	}

	// Eagerly build the default (no-proxy) client and validate the global proxy if set.
	_, err := manager.clientFor("")
	if err != nil {
		return nil, fmt.Errorf("default client: %w", err)
	}

	if cfg.Proxy != "" {
		_, err = manager.clientFor(cfg.Proxy)
		if err != nil {
			return nil, fmt.Errorf("global proxy %q: %w", cfg.Proxy, err)
		}
	}

	return manager, nil
}

// Matcher returns the currently-active matcher.
func (r *rulesetManager) Matcher() *Matcher {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.matcher
}

// TriggerReload schedules a forced reload (non-blocking, coalesced).
func (r *rulesetManager) TriggerReload() {
	select {
	case r.reloadCh <- struct{}{}:
	default:
	}
}

// Run loads all rulesets at startup, then refreshes them on configured intervals.
func (r *rulesetManager) Run(ctx context.Context) {
	r.refreshAll(ctx)

	type tickEvent struct{ idx int }

	tickCh := make(chan tickEvent, len(r.cfg.Rulesets))

	var wgroup sync.WaitGroup

	for idx, ruleset := range r.cfg.Rulesets {
		if ruleset.UpdateInterval <= 0 {
			continue
		}

		wgroup.Add(1)

		go func(idx int, interval time.Duration) {
			defer wgroup.Done()

			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					select {
					case tickCh <- tickEvent{idx: idx}:
					case <-ctx.Done():
						return
					}
				}
			}
		}(idx, ruleset.UpdateInterval)
	}

	for {
		select {
		case <-ctx.Done():
			wgroup.Wait()

			return
		case <-r.reloadCh:
			r.refreshAll(ctx)
		case event := <-tickCh:
			r.refreshOne(ctx, r.cfg.Rulesets[event.idx])
			r.rebuildMatcher()
		}
	}
}

func (r *rulesetManager) refreshAll(ctx context.Context) {
	var wgroup sync.WaitGroup

	for _, ruleset := range r.cfg.Rulesets {
		wgroup.Add(1)

		go func(ruleset RulesetEntry) {
			defer wgroup.Done()

			r.refreshOne(ctx, ruleset)
		}(ruleset)
	}

	wgroup.Wait()
	r.rebuildMatcher()
}

func (r *rulesetManager) refreshOne(ctx context.Context, ruleset RulesetEntry) {
	path := r.cachePath(ruleset.URL)
	proxyURL := r.effectiveProxy(ruleset)

	client, clientErr := r.clientFor(proxyURL)
	if clientErr != nil {
		log.Error().Err(clientErr).Str("url", ruleset.URL).Str("proxy", proxyURL).
			Msg("ruleset proxy invalid, skipping refresh")

		return
	}

	dlCtx, cancel := context.WithTimeout(ctx, rulesetTimeout)
	defer cancel()

	err := r.download(dlCtx, client, ruleset.URL, path)
	if err == nil {
		return
	}

	_, statErr := os.Stat(path)
	if statErr == nil {
		log.Warn().Err(err).Str("url", ruleset.URL).Str("proxy", proxyURL).
			Msg("ruleset refresh failed, using cached copy")

		return
	}

	log.Error().Err(err).Str("url", ruleset.URL).Str("proxy", proxyURL).
		Msg("ruleset download failed, no cached copy")
}

func (r *rulesetManager) rebuildMatcher() {
	matcher := NewMatcher()

	for _, ruleset := range r.cfg.Rulesets {
		path := r.cachePath(ruleset.URL)

		//nolint:gosec // path is derived from url hash inside cacheDir.
		raw, err := os.ReadFile(path)
		if err != nil {
			log.Warn().Err(err).Str("url", ruleset.URL).Msg("ruleset file unreadable")

			continue
		}

		geoList, decodeErr := decodeGeoSiteList(raw)
		if decodeErr != nil {
			log.Error().Err(decodeErr).Str("url", ruleset.URL).Msg("ruleset decode failed")

			continue
		}

		applyRuleset(matcher, geoList, ruleset)
	}

	r.mu.Lock()
	r.matcher = matcher
	r.mu.Unlock()

	stats := matcher.Stats()
	for tag, count := range stats {
		log.Info().Str("tag", tag).Int("domains", count).Msg("ruleset tag loaded")
	}
}

func decodeGeoSiteList(raw []byte) (*routercommon.GeoSiteList, error) {
	var geoList routercommon.GeoSiteList

	err := proto.Unmarshal(raw, &geoList)
	if err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return &geoList, nil
}

func applyRuleset(matcher *Matcher, geoList *routercommon.GeoSiteList, ruleset RulesetEntry) {
	tagIndex := make(map[string]string, len(ruleset.Tags))
	for _, tag := range ruleset.Tags {
		tagIndex[strings.ToLower(tag.Name)] = tag.Action
	}

	for _, site := range geoList.GetEntry() {
		code := strings.ToLower(site.GetCountryCode())

		action, ok := tagIndex[code]
		if !ok {
			continue
		}

		for _, dom := range site.GetDomain() {
			value := strings.TrimSpace(dom.GetValue())
			if value == "" {
				continue
			}

			addRule(matcher, dom.GetType(), value, action, code, ruleset.URL)
		}
	}
}

func addRule(matcher *Matcher, dtype routercommon.Domain_Type, value, action, tag, source string) {
	switch dtype {
	case routercommon.Domain_RootDomain:
		matcher.AddSuffix(value, action, tag, source)
	case routercommon.Domain_Full:
		matcher.AddFull(value, action, tag, source)
	case routercommon.Domain_Plain:
		matcher.AddKeyword(value, action, tag, source)
	case routercommon.Domain_Regex:
		err := matcher.AddRegex(value, action, tag, source)
		if err != nil {
			log.Warn().Err(err).Str("pattern", value).Str("tag", tag).Msg("regex compile failed")
		}
	}
}

// effectiveProxy resolves the per-ruleset proxy override against the global default.
// Empty ruleset proxy inherits Config.Proxy; ProxyDirect ("direct") forces no proxy.
func (r *rulesetManager) effectiveProxy(ruleset RulesetEntry) string {
	if ruleset.Proxy == ProxyDirect {
		return ""
	}

	if ruleset.Proxy != "" {
		return ruleset.Proxy
	}

	return r.cfg.Proxy
}

// clientFor returns a cached *http.Client wired with the given proxy URL.
// An empty proxyURL yields a direct-connection client.
func (r *rulesetManager) clientFor(proxyURL string) (*http.Client, error) {
	r.clientMu.Lock()
	defer r.clientMu.Unlock()

	client, ok := r.clients[proxyURL]
	if ok {
		return client, nil
	}

	transport, err := newProxyTransport(proxyURL)
	if err != nil {
		return nil, err
	}

	client = &http.Client{Timeout: rulesetTimeout, Transport: transport}
	r.clients[proxyURL] = client

	return client, nil
}

func newProxyTransport(proxyURL string) (*http.Transport, error) {
	transport := &http.Transport{}

	if proxyURL == "" {
		return transport, nil
	}

	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("parse proxy url: %w", err)
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(parsed)
	case "socks5", "socks5h":
		dialer, dialErr := proxy.FromURL(parsed, proxy.Direct)
		if dialErr != nil {
			return nil, fmt.Errorf("socks5 dialer: %w", dialErr)
		}

		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("%w: dialer does not support context", errUnsupportedProxy)
		}

		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return contextDialer.DialContext(ctx, network, address)
		}
	default:
		return nil, fmt.Errorf("%w: %s", errUnsupportedProxy, parsed.Scheme)
	}

	return transport, nil
}

func (r *rulesetManager) download(ctx context.Context, client *http.Client, source, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", rulesetUA)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%w: %s", errUnexpectedHTTPStatus, resp.Status)
	}

	return writeFileAtomic(dest, resp.Body)
}

func writeFileAtomic(dest string, body io.Reader) error {
	tmp := dest + ".tmp"

	//nolint:gosec // dest is internal cache path.
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, ruleFilePerm)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}

	copyErr := func() error {
		_, copyErr := io.Copy(file, body)
		if copyErr != nil {
			return fmt.Errorf("copy: %w", copyErr)
		}

		return nil
	}()

	closeErr := file.Close()

	if copyErr != nil {
		_ = os.Remove(tmp)

		return copyErr
	}

	if closeErr != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("close tmp: %w", closeErr)
	}

	renameErr := os.Rename(tmp, dest)
	if renameErr != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("rename: %w", renameErr)
	}

	return nil
}

func (r *rulesetManager) cachePath(source string) string {
	sum := sha1.Sum([]byte(source)) //nolint:gosec // not security-sensitive.
	name := hex.EncodeToString(sum[:]) + ".dat"

	return filepath.Join(r.cacheDir, name)
}
