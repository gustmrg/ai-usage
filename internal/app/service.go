package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/gustmrg/ai-usage/internal/cache"
	"github.com/gustmrg/ai-usage/internal/model"
	"github.com/gustmrg/ai-usage/internal/provider"
)

const (
	FreshFor = 5 * time.Minute
	MaxStale = 24 * time.Hour
)

type Result struct {
	Snapshot model.Snapshot
	Err      error
}

type Service struct {
	providers []provider.Provider
	cache     *cache.Cache
}

type staleCachePolicy interface {
	AllowStaleCache() bool
}

func NewService(cacheStore *cache.Cache, providers ...provider.Provider) *Service {
	return &Service{providers: providers, cache: cacheStore}
}

func (s *Service) Providers() []provider.Provider {
	return append([]provider.Provider(nil), s.providers...)
}

func (s *Service) Provider(id string) provider.Provider {
	for _, p := range s.providers {
		if p.ID() == id {
			return p
		}
	}
	return nil
}

func (s *Service) Fetch(ctx context.Context, id string, force bool) Result {
	p := s.Provider(id)
	if p == nil {
		return Result{Err: fmt.Errorf("unknown provider %q", id)}
	}
	if !p.Detect().Available {
		return Result{Err: &provider.Error{Kind: provider.ErrorCredentials, Provider: id, Message: p.Detect().Detail}}
	}
	cacheKey, err := p.CacheKey()
	if err != nil {
		return Result{Err: &provider.Error{Kind: provider.ErrorCredentials, Provider: id, Message: err.Error(), Err: err}}
	}

	if !force {
		if snapshot, ok, err := s.cache.Read(id, cacheKey, FreshFor); err == nil && ok {
			return Result{Snapshot: snapshot}
		}
	}
	if err := s.cache.Ensure(id); err != nil {
		return Result{Err: &provider.Error{Kind: provider.ErrorCache, Provider: id, Message: err.Error(), Err: err}}
	}

	lock := flock.New(s.cache.LockPath(id, cacheKey))
	lockCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	locked, err := lock.TryLockContext(lockCtx, 50*time.Millisecond)
	if err != nil {
		return s.fallback(id, cacheKey, fmt.Errorf("wait for provider lock: %w", err))
	}
	if !locked {
		return s.fallback(id, cacheKey, fmt.Errorf("timed out waiting for provider lock"))
	}
	defer lock.Unlock()

	if !force {
		if snapshot, ok, err := s.cache.Read(id, cacheKey, FreshFor); err == nil && ok {
			return Result{Snapshot: snapshot}
		}
	}

	snapshot, err := p.Fetch(ctx, cacheKey)
	if err != nil {
		return s.fallback(id, cacheKey, err)
	}
	snapshot.SchemaVersion = model.SchemaVersion
	snapshot.Provider = id
	snapshot.Stale = false
	snapshot.CacheAge = 0
	if snapshot.CollectedAt.IsZero() {
		snapshot.CollectedAt = time.Now().UTC()
	}
	if err := s.cache.Write(cacheKey, snapshot); err != nil {
		return Result{Snapshot: snapshot, Err: &provider.Error{Kind: provider.ErrorCache, Provider: id, Message: err.Error(), Err: err}}
	}
	return Result{Snapshot: snapshot}
}

func (s *Service) fallback(id, cacheKey string, fetchErr error) Result {
	if policy, ok := s.Provider(id).(staleCachePolicy); ok && !policy.AllowStaleCache() {
		return Result{Err: fetchErr}
	}
	snapshot, ok, cacheErr := s.cache.Read(id, cacheKey, MaxStale)
	if cacheErr == nil && ok {
		snapshot.Stale = true
		return Result{Snapshot: snapshot, Err: fetchErr}
	}
	return Result{Err: fetchErr}
}

func (s *Service) FetchAll(ctx context.Context, force bool) map[string]Result {
	results := make(map[string]Result, len(s.providers))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range s.providers {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := s.Fetch(ctx, p.ID(), force)
			mu.Lock()
			results[p.ID()] = result
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}
