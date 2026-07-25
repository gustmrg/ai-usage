package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gustmrg/ai-usage/internal/model"
)

func TestWriteReadAndExpiration(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := At(t.TempDir())
	store.now = func() time.Time { return now }
	snapshot := model.Snapshot{SchemaVersion: model.SchemaVersion, Provider: "codex", CollectedAt: now.Add(-time.Minute)}
	if err := store.Write("account-a", snapshot); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.Read("codex", "account-a", 5*time.Minute)
	if err != nil || !ok {
		t.Fatalf("Read() = (%v, %v, %v), want cache hit", got, ok, err)
	}
	if got.CacheAge != 60 {
		t.Fatalf("CacheAge = %d, want 60", got.CacheAge)
	}
	if _, ok, err := store.Read("codex", "account-a", 30*time.Second); err != nil || ok {
		t.Fatalf("expired Read() ok = %v, err = %v", ok, err)
	}
}

func TestReadRejectsWrongProvider(t *testing.T) {
	store := At(t.TempDir())
	if err := store.Write("account-a", model.Snapshot{SchemaVersion: model.SchemaVersion, Provider: "kimi", CollectedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.Path("kimi", "account-a"))
	if err != nil {
		t.Fatal(err)
	}
	path := store.Path("codex", "account-a")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Read("codex", "account-a", time.Hour); err == nil {
		t.Fatal("Read() succeeded with a cache for the wrong provider")
	}
}

func TestCacheKeysIsolateAccounts(t *testing.T) {
	store := At(t.TempDir())
	snapshot := model.Snapshot{SchemaVersion: model.SchemaVersion, Provider: "codex", CollectedAt: time.Now()}
	if err := store.Write("account-a", snapshot); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Read("codex", "account-b", time.Hour); err != nil || ok {
		t.Fatalf("second account cache hit = %v, err = %v", ok, err)
	}
}
