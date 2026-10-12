package llm

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// GuideService wraps a provider with a cache, request de-duplication, a
// timeout and a fallback. Guides only depend on (fault, component, location,
// language), so a cache hit rate close to 100% keeps LLM cost negligible at
// 2,000 stores.
//
// A remote call takes seconds while the store UI polls every 3 s, so the first
// request for a key starts the generation in the background and every caller
// (including the first) gets the template guide until the real one is ready.
type GuideService struct {
	primary  Provider
	fallback Provider
	timeout  time.Duration
	log      *slog.Logger

	mu       sync.Mutex
	cache    map[GuideRequest]*Guide
	inflight map[GuideRequest]bool
	failedAt map[GuideRequest]time.Time
}

const retryAfterFailure = time.Minute

func NewGuideService(primary Provider, timeout time.Duration, log *slog.Logger) *GuideService {
	return &GuideService{primary: primary, fallback: Template{}, timeout: timeout, log: log,
		cache: map[GuideRequest]*Guide{}, inflight: map[GuideRequest]bool{}, failedAt: map[GuideRequest]time.Time{}}
}

func (s *GuideService) ProviderName() string { return s.primary.Name() }

func (s *GuideService) Guide(ctx context.Context, req GuideRequest) *Guide {
	if _, isTemplate := s.primary.(Template); isTemplate {
		g, _ := s.primary.GenerateGuide(ctx, req)
		return g
	}

	s.mu.Lock()
	if g, ok := s.cache[req]; ok {
		s.mu.Unlock()
		return g
	}
	start := !s.inflight[req] && time.Since(s.failedAt[req]) > retryAfterFailure
	if start {
		s.inflight[req] = true
	}
	s.mu.Unlock()

	if start {
		go s.generate(req)
	}
	g, _ := s.fallback.GenerateGuide(ctx, req)
	return g
}

func (s *GuideService) generate(req GuideRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	t0 := time.Now()
	g, err := s.primary.GenerateGuide(ctx, req)

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, req)
	if err != nil {
		s.failedAt[req] = time.Now()
		s.log.Warn("llm guide failed, template stays in use", "provider", s.primary.Name(), "fault", req.FaultCode, "lang", req.Language, "err", err)
		return
	}
	if g.Language == "" {
		g.Language = req.Language
	}
	g.Source = s.primary.Name()
	s.cache[req] = g
	s.log.Info("llm guide generated", "provider", g.Source, "fault", req.FaultCode, "lang", req.Language, "dur", time.Since(t0).Round(time.Millisecond))
}
