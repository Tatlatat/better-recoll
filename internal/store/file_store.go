package store

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// FileStore implements the Store interface by persisting chunks to disk.
type FileStore struct {
	mu          sync.RWMutex
	dirPath     string
	chunks      []Chunk
	index       map[int64]int // Maps Chunk ID to its index in the chunks slice
	indexedDirs []string
}

// NewFileStore creates a new FileStore at the given directory path.
// It creates the directory if it is missing and reloads existing chunks.
func NewFileStore(path string) (*FileStore, error) {
	// Create directory if missing
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %q: %w", path, err)
	}

	fs := &FileStore{
		dirPath: path,
		index:   make(map[int64]int),
	}

	gobPath := filepath.Join(path, "chunks.gob")
	file, err := os.Open(gobPath)
	if err == nil {
		defer file.Close()
		var chunks []Chunk
		decoder := gob.NewDecoder(file)
		if err := decoder.Decode(&chunks); err != nil {
			// If file is empty (e.g. EOF), treat it as an empty store.
			// Otherwise return the decoding error.
			if err.Error() != "EOF" {
				return nil, fmt.Errorf("failed to decode chunks from %q: %w", gobPath, err)
			}
		}
		fs.chunks = chunks
		for i, c := range chunks {
			fs.index[c.ID] = i
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to open chunks file %q: %w", gobPath, err)
	}

	// Load indexed dirs if dirs.json exists
	dirsPath := filepath.Join(path, "dirs.json")
	if data, err := os.ReadFile(dirsPath); err == nil {
		var dirs []string
		if json.Unmarshal(data, &dirs) == nil {
			fs.indexedDirs = dirs
		}
	}

	return fs, nil
}

// Write appends chunks and persists them to disk.
func (fs *FileStore) Write(chunks []Chunk) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for _, c := range chunks {
		fs.chunks = append(fs.chunks, c)
		fs.index[c.ID] = len(fs.chunks) - 1
	}

	return fs.save()
}

// save writes the current chunks to the Gob file atomically.
func (fs *FileStore) save() error {
	gobPath := filepath.Join(fs.dirPath, "chunks.gob")
	tmpFile, err := os.CreateTemp(fs.dirPath, "chunks.*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
		}
		os.Remove(tmpName)
	}()

	encoder := gob.NewEncoder(tmpFile)
	if err := encoder.Encode(fs.chunks); err != nil {
		return fmt.Errorf("failed to encode chunks: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	tmpFile = nil // Clear so defer close doesn't log/fail

	if err := os.Rename(tmpName, gobPath); err != nil {
		return fmt.Errorf("failed to rename temp file to %q: %w", gobPath, err)
	}
	return nil
}

// AllVectors returns all vectors + ids in order.
func (fs *FileStore) AllVectors() (vectors [][]float32, ids []int64) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	vectors = make([][]float32, len(fs.chunks))
	ids = make([]int64, len(fs.chunks))
	for i, c := range fs.chunks {
		vectors[i] = c.Vector
		ids[i] = c.ID
	}
	return vectors, ids
}

// GetChunk returns the chunk with the given ID.
func (fs *FileStore) GetChunk(id int64) (Chunk, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	idx, ok := fs.index[id]
	if !ok {
		return Chunk{}, fmt.Errorf("chunk with ID %d not found", id)
	}
	return fs.chunks[idx], nil
}

// Count returns the number of chunks currently in the store.
func (fs *FileStore) Count() int {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	return len(fs.chunks)
}

// IndexedFilePaths returns the set of distinct file paths that already have
// chunks in the store. Used by the indexer to skip files already indexed,
// preventing duplicate chunks when overlapping directories are re-indexed.
func (fs *FileStore) IndexedFilePaths() map[string]bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	paths := make(map[string]bool)
	for _, c := range fs.chunks {
		paths[c.FilePath] = true
	}
	return paths
}

// Close flushes changes to disk. Since Write already persists directly,
// Close is a no-op that just returns nil.
func (fs *FileStore) Close() error {
	return nil
}

// Clear truncates/empties the persisted chunk+vector files.
func (fs *FileStore) Clear() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.chunks = nil
	fs.index = make(map[int64]int)
	fs.indexedDirs = nil

	// Remove dirs.json or save an empty JSON array
	dirsPath := filepath.Join(fs.dirPath, "dirs.json")
	_ = os.Remove(dirsPath)

	return fs.save()
}

// Compact rebuilds the store keeping only chunks for which keepChunk returns
// true, collapsing exact duplicates by (FilePath, Offset, Text), reassigning
// dense IDs from 0, and dropping indexed dirs for which keepDir returns false.
// Returns the surviving chunks (with new IDs) so the engine can rebuild its
// in-memory BM25/vector indexes. This is the permanent cleanup for the
// duplicate/junk index bug.
func (fs *FileStore) Compact(keepChunk func(Chunk) bool, keepDir func(string) bool) ([]Chunk, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Count/check if any chunk would be removed or deduped
	anyRemovedOrDeduped := false
	seenCheck := make(map[string]bool)
	for _, c := range fs.chunks {
		if !keepChunk(c) {
			anyRemovedOrDeduped = true
			break
		}
		key := c.FilePath + "\x00" + strconv.Itoa(c.Offset) + "\x00" + c.Text
		if seenCheck[key] {
			anyRemovedOrDeduped = true
			break
		}
		seenCheck[key] = true
	}

	// Check if any directory would be removed
	anyDirRemoved := false
	for _, d := range fs.indexedDirs {
		if !keepDir(d) {
			anyDirRemoved = true
			break
		}
	}

	if !anyRemovedOrDeduped && !anyDirRemoved {
		// No-op! Return existing chunks directly without rewriting files and without changing IDs.
		out := make([]Chunk, len(fs.chunks))
		copy(out, fs.chunks)
		return out, nil
	}

	seen := make(map[string]bool)
	var kept []Chunk
	newIndex := make(map[int64]int)
	var nextID int64
	for _, c := range fs.chunks {
		if !keepChunk(c) {
			continue
		}
		key := c.FilePath + "\x00" + strconv.Itoa(c.Offset) + "\x00" + c.Text
		if seen[key] {
			continue
		}
		seen[key] = true
		c.ID = nextID
		newIndex[nextID] = len(kept)
		nextID++
		kept = append(kept, c)
	}
	fs.chunks = kept
	fs.index = newIndex

	var keptDirs []string
	for _, d := range fs.indexedDirs {
		if keepDir(d) {
			keptDirs = append(keptDirs, d)
		}
	}
	fs.indexedDirs = keptDirs

	if err := fs.save(); err != nil {
		return nil, err
	}
	if err := fs.saveDirs(); err != nil {
		return nil, err
	}

	out := make([]Chunk, len(kept))
	copy(out, kept)
	return out, nil
}

// AddIndexedDir records a directory as indexed and persists the list.
func (fs *FileStore) AddIndexedDir(dir string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for _, d := range fs.indexedDirs {
		if d == dir {
			return nil
		}
	}

	fs.indexedDirs = append(fs.indexedDirs, dir)
	return fs.saveDirs()
}

// IndexedDirs returns the list of indexed directories.
func (fs *FileStore) IndexedDirs() []string {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	dirs := make([]string, len(fs.indexedDirs))
	copy(dirs, fs.indexedDirs)
	return dirs
}

func (fs *FileStore) saveDirs() error {
	dirsPath := filepath.Join(fs.dirPath, "dirs.json")
	data, err := json.Marshal(fs.indexedDirs)
	if err != nil {
		return fmt.Errorf("failed to marshal indexed dirs: %w", err)
	}

	tmpFile, err := os.CreateTemp(fs.dirPath, "dirs.*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file for dirs: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
		}
		os.Remove(tmpName)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write dirs data: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync dirs temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close dirs temp file: %w", err)
	}
	tmpFile = nil

	if err := os.Rename(tmpName, dirsPath); err != nil {
		return fmt.Errorf("failed to rename dirs temp file to %q: %w", dirsPath, err)
	}
	return nil
}
