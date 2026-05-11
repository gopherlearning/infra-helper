package dns

import (
	"context"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/rs/zerolog/log"
)

const defaultCacheTTL uint32 = 60

// resolver applies the matcher / fakeip / cache / upstream pipeline.
type resolver struct {
	cfg      Config
	rules    *rulesetManager
	fakeip   *fakeIPPool
	cache    *dnsCache
	upstream *upstreamPool
	metrics  *dnsMetrics
}

func newResolver(
	cfg Config,
	rules *rulesetManager,
	fakeip *fakeIPPool,
	cache *dnsCache,
	upstream *upstreamPool,
	metrics *dnsMetrics,
) *resolver {
	return &resolver{
		cfg: cfg, rules: rules, fakeip: fakeip,
		cache: cache, upstream: upstream, metrics: metrics,
	}
}

// Handle processes a single DNS request and returns a response.
func (r *resolver) Handle(ctx context.Context, req *dns.Msg) *dns.Msg {
	if len(req.Question) == 0 {
		return errResponse(req, dns.RcodeFormatError)
	}

	question := req.Question[0]
	qtypeStr := dns.TypeToString[question.Qtype]
	name := strings.ToLower(question.Name)

	match := r.rules.Matcher().Lookup(name)
	if match.Matched && match.Action != ActionDirect {
		return r.handleMatched(req, question, name, match, qtypeStr)
	}

	if match.Matched && match.Action == ActionDirect {
		r.metrics.queries.WithLabelValues(qtypeStr, "direct").Inc()
	}

	return r.handleUpstream(ctx, req, question, name, qtypeStr)
}

func (r *resolver) handleMatched(
	req *dns.Msg, question dns.Question, name string, match MatchResult, qtypeStr string,
) *dns.Msg {
	if match.Action == ActionBlock {
		r.metrics.blockHits.Inc()
		r.metrics.queries.WithLabelValues(qtypeStr, "block").Inc()

		return errResponse(req, dns.RcodeNameError)
	}

	return r.fakeipResponse(req, question, name, qtypeStr)
}

func (r *resolver) fakeipResponse(req *dns.Msg, question dns.Question, name, qtypeStr string) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.Authoritative = true

	switch question.Qtype {
	case dns.TypeA:
		if !r.fakeip.HasV4() {
			r.metrics.queries.WithLabelValues(qtypeStr, "fakeip_no_v4").Inc()

			return resp
		}

		addr := r.fakeip.ForA(name)
		record := &dns.A{
			Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: r.cfg.Fakeip.TTL},
			A:   addr.AsSlice(),
		}

		resp.Answer = append(resp.Answer, record)

		r.metrics.fakeipHits.Inc()
		r.metrics.queries.WithLabelValues(qtypeStr, "fakeip").Inc()
	case dns.TypeAAAA:
		if !r.fakeip.HasV6() {
			r.metrics.queries.WithLabelValues(qtypeStr, "fakeip_no_v6").Inc()

			return resp
		}

		addr := r.fakeip.ForAAAA(name)
		record := &dns.AAAA{
			Hdr:  dns.RR_Header{Name: question.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: r.cfg.Fakeip.TTL},
			AAAA: addr.AsSlice(),
		}

		resp.Answer = append(resp.Answer, record)

		r.metrics.fakeipHits.Inc()
		r.metrics.queries.WithLabelValues(qtypeStr, "fakeip").Inc()
	default:
		r.metrics.queries.WithLabelValues(qtypeStr, "fakeip_other").Inc()
	}

	return resp
}

func (r *resolver) handleUpstream(
	ctx context.Context, req *dns.Msg, question dns.Question, name, qtypeStr string,
) *dns.Msg {
	cached := r.lookupCache(question, name)
	if cached != nil {
		cached.SetReply(req)
		r.metrics.cacheHits.Inc()
		r.metrics.queries.WithLabelValues(qtypeStr, "cache_hit").Inc()

		return cached
	}

	r.metrics.cacheMisses.Inc()

	start := time.Now()

	resp, err := r.upstream.Exchange(ctx, req)
	if err != nil {
		log.Warn().Err(err).Str("name", name).Str("qtype", qtypeStr).Msg("upstream exchange failed")
		r.metrics.queries.WithLabelValues(qtypeStr, "servfail").Inc()
		r.metrics.upstreamErrors.WithLabelValues("aggregate").Inc()

		return errResponse(req, dns.RcodeServerFailure)
	}

	r.metrics.upstreamLat.WithLabelValues("aggregate").Observe(time.Since(start).Seconds())

	r.maybeCache(question, name, resp)
	r.metrics.queries.WithLabelValues(qtypeStr, "upstream").Inc()

	return resp
}

func (r *resolver) lookupCache(question dns.Question, name string) *dns.Msg {
	if r.cache == nil || !r.cfg.Cache.Enabled {
		return nil
	}

	return r.cache.Get(name, question.Qtype)
}

func (r *resolver) maybeCache(question dns.Question, name string, resp *dns.Msg) {
	if r.cache == nil || !r.cfg.Cache.Enabled || resp == nil {
		return
	}

	if resp.Rcode != dns.RcodeSuccess {
		return
	}

	ttl := minAnswerTTL(resp)
	if r.cfg.Cache.TTLOverride && ttl == 0 {
		ttl = defaultCacheTTL
	}

	r.cache.Put(name, question.Qtype, resp, ttl)
}

func errResponse(req *dns.Msg, rcode int) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetRcode(req, rcode)

	return resp
}
