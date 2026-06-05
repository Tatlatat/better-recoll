package index

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors a directory and its subdirectories recursively for file changes.
type Watcher struct {
	watcher  *fsnotify.Watcher
	onChange func(path string)
	done     chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex
	watched  map[string]bool
}

// NewWatcher creates and starts a new Watcher for the given directory.
// It watches the directory recursively and invokes onChange whenever a file is created or modified.
func NewWatcher(dir string, onChange func(path string)) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		watcher:  fw,
		onChange: onChange,
		done:     make(chan struct{}),
		watched:  make(map[string]bool),
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		fw.Close()
		return nil, err
	}

	err = w.addRecursive(absDir)
	if err != nil {
		fw.Close()
		return nil, err
	}

	w.wg.Add(1)
	go w.run()

	return w, nil
}

// addRecursive walks the directory tree starting from root and adds all subdirectories to the watcher.
// It ignores hidden directories (starting with ".").
func (w *Watcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip directories we cannot access, but do not fail the whole watch setup
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			// Skip hidden directories like .git, .sfsindex, etc.
			if name != "." && name != ".." && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}

			w.mu.Lock()
			already := w.watched[path]
			if !already {
				w.watched[path] = true
				w.mu.Unlock()
				err = w.watcher.Add(path)
				if err != nil {
					w.mu.Lock()
					delete(w.watched, path)
					w.mu.Unlock()
					return err
				}
			} else {
				w.mu.Unlock()
			}
		}
		return nil
	})
}

// run processes fsnotify events.
func (w *Watcher) run() {
	defer w.wg.Done()
	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// We care about created or modified files.
			if event.Has(fsnotify.Create) {
				fi, err := os.Stat(event.Name)
				if err == nil {
					if fi.IsDir() {
						if !strings.HasPrefix(fi.Name(), ".") {
							_ = w.addRecursive(event.Name)
						}
					} else if fi.Mode().IsRegular() {
						// Skip hidden files like .DS_Store
						if !strings.HasPrefix(fi.Name(), ".") {
							w.onChange(event.Name)
						}
					}
				}
			} else if event.Has(fsnotify.Write) {
				fi, err := os.Stat(event.Name)
				if err == nil && fi.Mode().IsRegular() {
					// Skip hidden files like .DS_Store
					if !strings.HasPrefix(fi.Name(), ".") {
						w.onChange(event.Name)
					}
				}
			} else if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				w.mu.Lock()
				delete(w.watched, event.Name)
				w.mu.Unlock()
			}
		case _, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// Close stops the Watcher.
func (w *Watcher) Close() error {
	close(w.done)
	err := w.watcher.Close()
	w.wg.Wait()
	return err
}
