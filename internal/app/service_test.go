package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gustmrg/ai-usage/internal/cache"
	"github.com/gustmrg/ai-usage/internal/model"
	"github.com/gustmrg/ai-usage/internal/provider"
)

type fakeProvider struct {
	fetches int
	err     error
}

type noStaleProvider struct {
	*fakeProvider
}

func (*noStaleProvider) AllowStaleCache() bool { return false }

func (p *fakeProvider) ID() string                { return "fake" }
func (p *fakeProvider) DisplayName() string       { return "Fake" }
func (p *fakeProvider) CacheKey() (string, error) { return "account", nil }
func (p *fakeProvider) Detect() provider.Detection {
	return provider.Detection{Available: true}
}
func (p *fakeProvider) Fetch(context.Context, string) (model.Snapshot, error) {
	p.fetches++
	if p.err != nil {
		return model.Snapshot{}, p.err
	}
	return model.Snapshot{Plan: "Test", CollectedAt: time.Now().UTC()}, nil
}

func TestFetchCreatesLockDirectoryAndReusesCache(t *testing.T) {
	p := &fakeProvider{}
	service := NewService(cache.At(t.TempDir()), p)
	first := service.Fetch(context.Background(), "fake", false)
	if first.Err != nil {
		t.Fatal(first.Err)
	}
	second := service.Fetch(context.Background(), "fake", false)
	if second.Err != nil {
		t.Fatal(second.Err)
	}
	if p.fetches != 1 {
		t.Fatalf("fetches = %d, want 1", p.fetches)
	}
}

func TestFetchReturnsStaleSnapshotOnProviderFailure(t *testing.T) {
	p := &fakeProvider{}
	service := NewService(cache.At(t.TempDir()), p)
	if result := service.Fetch(context.Background(), "fake", false); result.Err != nil {
		t.Fatal(result.Err)
	}
	p.err = errors.New("offline")
	result := service.Fetch(context.Background(), "fake", true)
	if result.Err == nil || !result.Snapshot.Stale {
		t.Fatalf("result = %#v, want stale snapshot and error", result)
	}
}

func TestFetchCanDisableStaleSnapshotOnProviderFailure(t *testing.T) {
	p := &noStaleProvider{fakeProvider: &fakeProvider{}}
	service := NewService(cache.At(t.TempDir()), p)
	if result := service.Fetch(context.Background(), "fake", false); result.Err != nil {
		t.Fatal(result.Err)
	}
	p.err = errors.New("offline")
	result := service.Fetch(context.Background(), "fake", true)
	if result.Err == nil || !result.Snapshot.CollectedAt.IsZero() {
		t.Fatalf("result = %#v, want error without stale snapshot", result)
	}
}
