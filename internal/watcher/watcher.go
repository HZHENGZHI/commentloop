// Package watcher watches a directory tree for Markdown changes.
package watcher

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/hzhengzhi/commentloop/internal/ignore"
)

// Event describes one debounced filesystem change, relative to the root.
type Event struct {
	Rel     string
	IsDir   bool
	Removed bool
}

// Watcher recursively watches a root directory.
type Watcher struct {
	fsw     *fsnotify.Watcher
	root    string
	onEvent func(Event)

	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// New starts watching root recursively. onEvent is called from a goroutine
// owned by the watcher; it must not block for long.
func New(root string, onEvent func(Event)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fsw:     fsw,
		root:    root,
		onEvent: onEvent,
		done:    make(chan struct{}),
	}
	if err := w.addRecursive(root); err != nil {
		_ = fsw.Close()
		return nil, err
	}
	w.wg.Add(1)
	go w.loop()
	return w, nil
}

// Close stops the watcher and releases its resources.
func (w *Watcher) Close() error {
	w.closeOnce.Do(func() {
		close(w.done)
		_ = w.fsw.Close()
	})
	w.wg.Wait()
	return nil
}

func (w *Watcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if p != root && ignore.SkipDir(d.Name()) {
			return filepath.SkipDir
		}
		if e := w.fsw.Add(p); e != nil {
			log.Printf("commentloop: watch %s: %v", p, e)
		}
		return nil
	})
}

func (w *Watcher) loop() {
	defer w.wg.Done()

	const debounce = 150 * time.Millisecond
	var timer *time.Timer
	var timerC <-chan time.Time
	pending := make(map[string]Event)

	flush := func() {
		if len(pending) == 0 {
			return
		}
		events := make([]Event, 0, len(pending))
		for _, ev := range pending {
			events = append(events, ev)
		}
		pending = make(map[string]Event)
		timerC = nil
		for _, ev := range events {
			w.onEvent(ev)
		}
	}

	for {
		select {
		case <-w.done:
			if timer != nil {
				timer.Stop()
			}
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			translated, ok := w.translate(ev)
			if !ok {
				continue
			}
			pending[translated.Rel] = translated
			if timer == nil {
				timer = time.NewTimer(debounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(debounce)
			}
			timerC = timer.C
		case <-timerC:
			flush()
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *Watcher) translate(ev fsnotify.Event) (Event, bool) {
	if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
		return Event{}, false
	}
	rel, err := filepath.Rel(w.root, ev.Name)
	if err != nil || rel == "." {
		return Event{}, false
	}
	rel = filepath.ToSlash(rel)
	base := filepath.Base(rel)
	if ignore.IsTemp(base) {
		return Event{}, false
	}

	info, statErr := os.Stat(ev.Name)
	if statErr == nil && info.IsDir() {
		if ignore.SkipDir(base) {
			return Event{}, false
		}
		switch {
		case ev.Op&fsnotify.Create != 0:
			// Watch the new directory and everything below it.
			if err := w.addRecursive(ev.Name); err != nil {
				log.Printf("commentloop: add watch %s: %v", ev.Name, err)
			}
			return Event{Rel: rel, IsDir: true}, true
		case ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0:
			return Event{Rel: rel, IsDir: true, Removed: true}, true
		default:
			return Event{}, false
		}
	}

	if !ignore.IsMarkdown(rel) {
		return Event{}, false
	}

	if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 && (statErr != nil || info == nil) {
		return Event{Rel: rel, Removed: true}, true
	}
	return Event{Rel: rel, Removed: statErr != nil}, true
}
