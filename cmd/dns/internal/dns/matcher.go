package dns

import (
	"fmt"
	"maps"
	"regexp"
	"strings"
	"sync"
)

// Action ranks (higher = wins). block > fakeip > direct.
const (
	rankNone   = 0
	rankDirect = 1
	rankFakeIP = 2
	rankBlock  = 3
)

// actionRank assigns a priority order so we can compare matches.
func actionRank(action string) int {
	switch action {
	case ActionBlock:
		return rankBlock
	case ActionFakeIP:
		return rankFakeIP
	case ActionDirect:
		return rankDirect
	default:
		return rankNone
	}
}

// MatchResult carries the resolved action plus diagnostic info.
type MatchResult struct {
	Action  string
	Tag     string
	Source  string
	Matched bool
}

// suffixNode is a single trie node keyed by domain label.
type suffixNode struct {
	children map[string]*suffixNode
	action   string
	tag      string
	source   string
}

func newSuffixNode() *suffixNode {
	return &suffixNode{children: make(map[string]*suffixNode)}
}

// Matcher answers domain → action lookups across loaded rulesets.
type Matcher struct {
	mu       sync.RWMutex
	suffixes *suffixNode
	full     map[string]MatchResult
	keywords []ruleString
	regexps  []ruleRegex
	stats    map[string]int
}

type ruleString struct {
	value  string
	action string
	tag    string
	source string
}

type ruleRegex struct {
	expr   *regexp.Regexp
	action string
	tag    string
	source string
}

// NewMatcher returns an empty matcher.
func NewMatcher() *Matcher {
	return &Matcher{
		suffixes: newSuffixNode(),
		full:     make(map[string]MatchResult),
		stats:    make(map[string]int),
	}
}

// AddSuffix registers a domain-suffix rule.
func (m *Matcher) AddSuffix(domain, action, tag, source string) {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	if domain == "" {
		return
	}

	labels := splitLabels(domain)
	node := m.suffixes

	for i := len(labels) - 1; i >= 0; i-- {
		next, ok := node.children[labels[i]]
		if !ok {
			next = newSuffixNode()
			node.children[labels[i]] = next
		}

		node = next
	}

	if actionRank(action) > actionRank(node.action) {
		node.action = action
		node.tag = tag
		node.source = source
	}

	m.bump(tag)
}

// AddFull registers a full (exact) domain rule.
func (m *Matcher) AddFull(domain, action, tag, source string) {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	if domain == "" {
		return
	}

	cur, ok := m.full[domain]
	if !ok || actionRank(action) > actionRank(cur.Action) {
		m.full[domain] = MatchResult{Action: action, Tag: tag, Source: source, Matched: true}
	}

	m.bump(tag)
}

// AddKeyword registers a substring rule.
func (m *Matcher) AddKeyword(value, action, tag, source string) {
	value = strings.ToLower(value)
	if value == "" {
		return
	}

	m.keywords = append(m.keywords, ruleString{
		value: value, action: action, tag: tag, source: source,
	})

	m.bump(tag)
}

// AddRegex registers a regex rule. Returns error if pattern doesn't compile.
func (m *Matcher) AddRegex(pattern, action, tag, source string) error {
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("compile regex: %w", err)
	}

	m.regexps = append(m.regexps, ruleRegex{
		expr: compiled, action: action, tag: tag, source: source,
	})

	m.bump(tag)

	return nil
}

// Stats returns a snapshot of per-tag domain counts.
func (m *Matcher) Stats() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]int, len(m.stats))
	maps.Copy(out, m.stats)

	return out
}

// Lookup returns the highest-priority action for the FQDN, or Matched=false.
func (m *Matcher) Lookup(fqdn string) MatchResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	domain := strings.ToLower(strings.TrimSuffix(fqdn, "."))

	best := MatchResult{}

	if result, ok := m.full[domain]; ok && actionRank(result.Action) > actionRank(best.Action) {
		best = result
	}

	if result, ok := m.lookupSuffix(domain); ok && actionRank(result.Action) > actionRank(best.Action) {
		best = result
	}

	for _, keyword := range m.keywords {
		if !strings.Contains(domain, keyword.value) {
			continue
		}

		if actionRank(keyword.action) > actionRank(best.Action) {
			best = MatchResult{Action: keyword.action, Tag: keyword.tag, Source: keyword.source, Matched: true}
		}
	}

	for _, rule := range m.regexps {
		if !rule.expr.MatchString(domain) {
			continue
		}

		if actionRank(rule.action) > actionRank(best.Action) {
			best = MatchResult{Action: rule.action, Tag: rule.tag, Source: rule.source, Matched: true}
		}
	}

	return best
}

func (m *Matcher) bump(tag string) {
	m.stats[tag]++
}

func (m *Matcher) lookupSuffix(domain string) (MatchResult, bool) {
	labels := splitLabels(domain)
	node := m.suffixes

	var best MatchResult

	matched := false

	for i := len(labels) - 1; i >= 0; i-- {
		next, ok := node.children[labels[i]]
		if !ok {
			break
		}

		node = next

		if node.action != "" && actionRank(node.action) > actionRank(best.Action) {
			best = MatchResult{Action: node.action, Tag: node.tag, Source: node.source, Matched: true}
			matched = true
		}
	}

	return best, matched
}

func splitLabels(domain string) []string {
	if domain == "" {
		return nil
	}

	return strings.Split(domain, ".")
}
