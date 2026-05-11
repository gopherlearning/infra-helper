//nolint:testpackage // helper for re-exporting internal symbols.
package dns

import "github.com/miekg/dns"

func buildMinimalAMsg(name string, ttl uint32) *dns.Msg {
	msg := new(dns.Msg)
	msg.Question = []dns.Question{{Name: name, Qtype: dns.TypeA, Qclass: dns.ClassINET}}
	msg.Answer = []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
			A:   []byte{1, 2, 3, 4},
		},
	}

	return msg
}

func buildMsgWithTTLs(ttls []uint32) *dns.Msg {
	msg := new(dns.Msg)

	for _, ttl := range ttls {
		msg.Answer = append(msg.Answer, &dns.A{Hdr: dns.RR_Header{Ttl: ttl}})
	}

	return msg
}
