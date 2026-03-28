package config

import (
	"log/slog"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors a config file for changes and calls a reload function.
// It debounces rapid changes to avoid reloading multiple times for a single
// save operation (editors often write + rename in quick succession).
type Watcher struct {
	path      string
	reloadFn  func(string) error
	watcher   *fsnotify.Watcher
	stopCh    chan struct{}
	stopOnce  sync.Once
}

// NewWatcher creates a new file watcher for the given config path.
func NewWatcher(path string, reloadFn func(string) error) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := w.Add(path); err != nil {
		w.Close()
		return nil, err
	}

	return &Watcher{
		path:     path,
		reloadFn: reloadFn,
		watcher:  w,
		stopCh:   make(chan struct{}),
	}, nil
}

// Start begins watching for file changes in a background goroutine.
func (fw *Watcher) Start() {
	go fw.loop()
}

// Stop stops the file watcher.
func (fw *Watcher) Stop() {
	fw.stopOnce.Do(func() {
		close(fw.stopCh)
		fw.watcher.Close()
	})
}

func (fw *Watcher) loop() {
	// Debounce: wait 500ms after last event before reloading.
	var debounce *time.Timer

	for {
		select {
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			// Only react to write and create events (rename = new file in place).
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(500*time.Millisecond, func() {
					slog.Info("config file changed, reloading", "path", fw.path)
					if err := fw.reloadFn(fw.path); err != nil {
						slog.Error("config reload failed", "error", err)
					} else {
						slog.Info("config reloaded successfully")
					}
				})
			}

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			slog.Error("config watcher error", "error", err)

		case <-fw.stopCh:
			if debounce != nil {
				debounce.Stop()
			}
			return
		}
	}
}
