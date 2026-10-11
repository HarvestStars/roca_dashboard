package llm

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// GuideService wraps a provider with a cache, a timeout and a fallback.
// Guides only depend on (fault, component, location, language), so a cache
// hit rate close to 100% keeps LLM cost negligible at 2,000 stores.
type GuideService struct {
	primary  Provider
	fallback Provider
	timeout  time.Duration
	log      *slog.Logger

	mu    sync.Mutex
	cache map[GuideRequest]*Guide
}

func NewGuideService(primary Provider, timeout time.Duration, log *slog.Logger) *GuideService {
	return &GuideService{primary: primary, fallback: Template{}, timeout: timeout, log: log, cache: map[GuideRequest]*Guide{}}
}

func (s *GuideService) ProviderName() string { return s.primary.Name() }

func (s *GuideService) Guide(ctx context.Context, req GuideRequest) *Guide {
	s.mu.Lock()
	if g, ok := s.cache[req]; ok {
		s.mu.Unlock()
		return g
	}
	s.mu.Unlock()

	cctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	g, err := s.primary.GenerateGuide(cctx, req)
	if err != nil {
		s.log.Warn("llm provider failed, using template fallback", "provider", s.primary.Name(), "err", err)
		// Fallback result is not cached so a recovered provider is used next time.
		g, _ = s.fallback.GenerateGuide(ctx, req)
		return g
	}
	if g.Language == "" {
		g.Language = req.Language
	}
	g.Source = s.primary.Name()
	s.mu.Lock()
	s.cache[req] = g
	s.mu.Unlock()
	return g
}
