package tool

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type FileReadState struct {
	Path       string          `json:"path"`
	Exists     bool            `json:"exists"`
	ContentSHA [32]byte        `json:"-"`
	Mode       os.FileMode     `json:"mode"`
	Symlink    bool            `json:"symlink"`
	Size       int64           `json:"size"`
	FullRead   bool            `json:"full_read"`
	TotalLines int             `json:"total_lines,omitempty"`
	Ranges     []FileReadRange `json:"ranges,omitempty"`
}

type FileReadRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

func NewFileReadState(path string, content []byte, info os.FileInfo, fullRead bool) (FileReadState, error) {
	if path == "" || !filepath.IsAbs(path) {
		return FileReadState{}, errors.New("file read state path is not canonical")
	}
	state := FileReadState{Path: filepath.Clean(path), Exists: info != nil, FullRead: fullRead}
	if info != nil {
		state.ContentSHA = sha256.Sum256(content)
		state.Mode = info.Mode()
		state.Symlink = info.Mode()&os.ModeSymlink != 0
		state.Size = info.Size()
	}
	return state, nil
}

func (state FileReadState) Matches(content []byte, info os.FileInfo) bool {
	if !state.Exists {
		return info == nil
	}
	if info == nil || info.Mode() != state.Mode || info.Size() != state.Size {
		return false
	}
	return sha256.Sum256(content) == state.ContentSHA
}

type FileReadStateStore struct {
	mutex  sync.RWMutex
	states map[string]FileReadState
}

func NewFileReadStateStore() *FileReadStateStore {
	return &FileReadStateStore{states: make(map[string]FileReadState)}
}

func (store *FileReadStateStore) Record(state FileReadState) {
	if store == nil || state.Path == "" || !filepath.IsAbs(state.Path) {
		return
	}
	store.mutex.Lock()
	state.Ranges = append([]FileReadRange(nil), state.Ranges...)
	store.states[filepath.Clean(state.Path)] = state
	store.mutex.Unlock()
}

func (store *FileReadStateStore) Get(path string) (FileReadState, bool) {
	if store == nil || path == "" {
		return FileReadState{}, false
	}
	store.mutex.RLock()
	state, ok := store.states[filepath.Clean(path)]
	state.Ranges = append([]FileReadRange(nil), state.Ranges...)
	store.mutex.RUnlock()
	return state, ok
}

func (store *FileReadStateStore) RecordRange(state FileReadState, start, end, totalLines int) {
	if store == nil || state.Path == "" || !filepath.IsAbs(state.Path) || start <= 0 || end < start || totalLines < end {
		return
	}
	state.FullRead = false
	state.TotalLines = totalLines
	state.Ranges = []FileReadRange{{Start: start, End: end}}
	path := filepath.Clean(state.Path)
	store.mutex.Lock()
	if previous, ok := store.states[path]; ok && previous.sameSnapshot(state) {
		if previous.FullRead {
			store.mutex.Unlock()
			return
		}
		if previous.TotalLines == totalLines {
			state.Ranges = append(state.Ranges, previous.Ranges...)
		}
	}
	state.Ranges = mergeFileReadRanges(state.Ranges)
	state.FullRead = totalLines == 0 || len(state.Ranges) == 1 && state.Ranges[0].Start == 1 && state.Ranges[0].End == totalLines
	store.states[path] = state
	store.mutex.Unlock()
}

func (state FileReadState) sameSnapshot(other FileReadState) bool {
	return state.Exists == other.Exists && state.ContentSHA == other.ContentSHA && state.Mode == other.Mode && state.Symlink == other.Symlink && state.Size == other.Size
}

func mergeFileReadRanges(ranges []FileReadRange) []FileReadRange {
	if len(ranges) == 0 {
		return nil
	}
	sort.Slice(ranges, func(left, right int) bool {
		if ranges[left].Start == ranges[right].Start {
			return ranges[left].End < ranges[right].End
		}
		return ranges[left].Start < ranges[right].Start
	})
	merged := make([]FileReadRange, 0, len(ranges))
	for _, current := range ranges {
		last := len(merged) - 1
		if last >= 0 && current.Start <= merged[last].End+1 {
			if current.End > merged[last].End {
				merged[last].End = current.End
			}
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func (store *FileReadStateStore) Clear() {
	if store == nil {
		return
	}
	store.mutex.Lock()
	store.states = make(map[string]FileReadState)
	store.mutex.Unlock()
}
