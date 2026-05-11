package dns_test

import (
	"testing"

	dnssvc "infra.helper/cmd/dns/internal/dns"
)

func TestMatcherSuffix(t *testing.T) {
	t.Parallel()

	matcher := dnssvc.NewMatcher()
	matcher.AddSuffix("example.com", dnssvc.ActionFakeIP, "test", "src")

	cases := []struct {
		fqdn string
		want bool
	}{
		{"example.com", true},
		{"www.example.com", true},
		{"deep.sub.example.com", true},
		{"badexample.com", false},
		{"example.org", false},
	}

	for _, testCase := range cases {
		got := matcher.Lookup(testCase.fqdn)
		if got.Matched != testCase.want {
			t.Errorf("Lookup(%q): matched=%v want=%v", testCase.fqdn, got.Matched, testCase.want)
		}
	}
}

func TestMatcherActionPriority(t *testing.T) {
	t.Parallel()

	matcher := dnssvc.NewMatcher()
	matcher.AddSuffix("example.com", dnssvc.ActionDirect, "direct-tag", "src")
	matcher.AddFull("ads.example.com", dnssvc.ActionBlock, "block-tag", "src")
	matcher.AddSuffix("ads.example.com", dnssvc.ActionFakeIP, "fakeip-tag", "src")

	got := matcher.Lookup("ads.example.com")
	if got.Action != dnssvc.ActionBlock {
		t.Errorf("expected block to win, got %s", got.Action)
	}
}

func TestMatcherFull(t *testing.T) {
	t.Parallel()

	matcher := dnssvc.NewMatcher()
	matcher.AddFull("exact.com", dnssvc.ActionFakeIP, "tag", "src")

	if !matcher.Lookup("exact.com").Matched {
		t.Error("exact match expected to hit")
	}

	if matcher.Lookup("sub.exact.com").Matched {
		t.Error("full match must not match subdomains")
	}
}

func TestMatcherKeyword(t *testing.T) {
	t.Parallel()

	matcher := dnssvc.NewMatcher()
	matcher.AddKeyword("youtube", dnssvc.ActionFakeIP, "tag", "src")

	if !matcher.Lookup("foo.youtube.com").Matched {
		t.Error("keyword should match substring")
	}

	if matcher.Lookup("notrelated.com").Matched {
		t.Error("keyword must not match unrelated")
	}
}

func TestMatcherRegex(t *testing.T) {
	t.Parallel()

	matcher := dnssvc.NewMatcher()

	err := matcher.AddRegex(`^ads\d+\.example\.com$`, dnssvc.ActionBlock, "tag", "src")
	if err != nil {
		t.Fatalf("regex add: %v", err)
	}

	if !matcher.Lookup("ads42.example.com").Matched {
		t.Error("regex should match ads42")
	}

	if matcher.Lookup("adsX.example.com").Matched {
		t.Error("regex should not match adsX")
	}
}

func TestMatcherStats(t *testing.T) {
	t.Parallel()

	matcher := dnssvc.NewMatcher()
	matcher.AddSuffix("a.com", dnssvc.ActionFakeIP, "tag1", "src")
	matcher.AddFull("b.com", dnssvc.ActionFakeIP, "tag1", "src")
	matcher.AddKeyword("c", dnssvc.ActionFakeIP, "tag2", "src")

	stats := matcher.Stats()
	if stats["tag1"] != 2 || stats["tag2"] != 1 {
		t.Errorf("unexpected stats: %v", stats)
	}
}
