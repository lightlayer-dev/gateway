package config

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatcherDetectsChanges(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gateway.yaml")

	// Write initial config.
	require.NoError(t, os.WriteFile(cfgPath, []byte("gateway:\n  origin:\n    url: https://old.example.com\n"), 0644))

	var reloadCount atomic.Int32
	reloadFn := func(path string) error {
		reloadCount.Add(1)
		return nil
	}

	w, err := NewWatcher(cfgPath, reloadFn)
	require.NoError(t, err)
	w.Start()
	defer w.Stop()

	// Give the watcher time to initialize.
	time.Sleep(100 * time.Millisecond)

	// Modify the file.
	require.NoError(t, os.WriteFile(cfgPath, []byte("gateway:\n  origin:\n    url: https://new.example.com\n"), 0644))

	// Wait for debounce (500ms) + some buffer.
	assert.Eventually(t, func() bool {
		return reloadCount.Load() >= 1
	}, 3*time.Second, 100*time.Millisecond, "watcher should have triggered reload")
}

func TestWatcherStopsCleanly(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gateway.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("test"), 0644))

	w, err := NewWatcher(cfgPath, func(string) error { return nil })
	require.NoError(t, err)
	w.Start()

	// Should not panic on double stop.
	w.Stop()
	w.Stop()
}
