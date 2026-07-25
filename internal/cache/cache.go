package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gustmrg/ai-usage/internal/model"
)

type Cache struct {
	root string
	now  func() time.Time
}

func New() (*Cache, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user cache directory: %w", err)
	}
	return At(filepath.Join(root, "ai-usage")), nil
}

func At(root string) *Cache {
	return &Cache{root: root, now: time.Now}
}

func (c *Cache) Root() string { return c.root }

func (c *Cache) Path(providerID, cacheKey string) string {
	return filepath.Join(c.root, providerID, cacheKey+".json")
}

func (c *Cache) LockPath(providerID, cacheKey string) string {
	return filepath.Join(c.root, providerID, "."+cacheKey+".lock")
}

func (c *Cache) Ensure(providerID string) error {
	if err := os.MkdirAll(filepath.Join(c.root, providerID), 0o700); err != nil {
		return fmt.Errorf("create provider cache directory: %w", err)
	}
	return nil
}

func (c *Cache) Read(providerID, cacheKey string, maxAge time.Duration) (model.Snapshot, bool, error) {
	path := c.Path(providerID, cacheKey)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return model.Snapshot{}, false, nil
	}
	if err != nil {
		return model.Snapshot{}, false, fmt.Errorf("read cache: %w", err)
	}

	var snapshot model.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return model.Snapshot{}, false, fmt.Errorf("decode cache: %w", err)
	}
	if snapshot.SchemaVersion != model.SchemaVersion || snapshot.Provider != providerID || snapshot.CollectedAt.IsZero() {
		return model.Snapshot{}, false, fmt.Errorf("cache has an incompatible schema")
	}
	age := c.now().Sub(snapshot.CollectedAt)
	if age < 0 {
		age = 0
	}
	if age > maxAge {
		return model.Snapshot{}, false, nil
	}
	snapshot.CacheAge = int64(age.Seconds())
	return snapshot, true, nil
}

func (c *Cache) Write(cacheKey string, snapshot model.Snapshot) error {
	dir := filepath.Dir(c.Path(snapshot.Provider, cacheKey))
	if err := c.Ensure(snapshot.Provider); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cache: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".snapshot-*")
	if err != nil {
		return fmt.Errorf("create cache temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure cache temporary file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write cache temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync cache temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close cache temporary file: %w", err)
	}
	if err := os.Rename(tmpPath, c.Path(snapshot.Provider, cacheKey)); err != nil {
		return fmt.Errorf("replace cache: %w", err)
	}
	return nil
}
