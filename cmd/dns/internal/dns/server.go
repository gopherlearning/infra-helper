package dns

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/miekg/dns"
	"github.com/rs/zerolog/log"
	"infra.helper/pkg/app"
)

const (
	dnsShutdownTimeout = 5 * time.Second
	dnsHandleTimeout   = 8 * time.Second
)

var errInvalidProtocol = errors.New("invalid protocol")

// dnsServer wraps a UDP and/or TCP miekg/dns Server bound to the same handler.
type dnsServer struct {
	cfg      ServerConfig
	resolver *resolver
}

func newDNSServer(cfg ServerConfig, resolver *resolver) *dnsServer {
	return &dnsServer{cfg: cfg, resolver: resolver}
}

// Run starts the configured listeners; each runs in its own goroutine.
// Run returns immediately; the listeners are tied to the parent app context.
func (s *dnsServer) Run(parent context.Context) error {
	handler := dns.HandlerFunc(func(writer dns.ResponseWriter, req *dns.Msg) {
		s.handle(parent, writer, req)
	})

	mux := dns.NewServeMux()
	mux.Handle(".", handler)

	for _, proto := range s.cfg.Protocols {
		switch proto {
		case "udp":
			go s.serve(parent, "udp", mux)
		case "tcp":
			go s.serve(parent, "tcp", mux)
		default:
			return fmt.Errorf("%w: protocol %q", errInvalidProtocol, proto)
		}
	}

	return nil
}

func (s *dnsServer) serve(parent context.Context, network string, mux dns.Handler) {
	jobName := "dns.server." + network
	ctx, onStop := app.AddJob(jobName)

	defer onStop()

	srv := &dns.Server{
		Addr:    s.cfg.Listen,
		Net:     network,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), dnsShutdownTimeout)
		defer cancel()

		err := srv.ShutdownContext(shutdownCtx)
		if err != nil {
			log.Error().Err(err).Str("net", network).Msg("dns shutdown failed")
		}
	}()

	log.Info().Str("addr", s.cfg.Listen).Str("net", network).Msg("dns listener started")

	serveErr := srv.ListenAndServe()
	if serveErr != nil {
		log.Error().Err(serveErr).Str("net", network).Msg("dns listener exited")
		app.Cancel()
	}
}

// handle dispatches a DNS request through the resolver pipeline.
func (s *dnsServer) handle(parent context.Context, writer dns.ResponseWriter, req *dns.Msg) {
	ctx, cancel := context.WithTimeout(parent, dnsHandleTimeout)
	defer cancel()

	resp := s.resolver.Handle(ctx, req)
	if resp == nil {
		resp = errResponse(req, dns.RcodeServerFailure)
	}

	writeErr := writer.WriteMsg(resp)
	if writeErr != nil {
		log.Debug().Err(writeErr).Msg("dns write failed")
	}
}
