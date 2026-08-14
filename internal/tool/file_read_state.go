package tool

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type FileReadState struct {
	Path       string      `json:"path"`
	Exists     bool        `json:"exists"`
	ContentSHA [32]byte    `json:"-"`
	Mode       os.FileMode `json:"mode"`
	Symlink    bool        `json:"symlink"`
	Size       int64       `json:"size"`
	FullRead   bool        `json:"full_read"`
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
	store.states[filepath.Clean(state.Path)] = state
	store.mutex.Unlock()
}

func (store *FileReadStateStore) Get(path string) (FileReadState, bool) {
	if store == nil || path == "" {
		return FileReadState{}, false
	}
	store.mutex.RLock()
	state, ok := store.states[filepath.Clean(path)]
	store.mutex.RUnlock()
	return state, ok
}

func (store *FileReadStateStore) Clear() {
	if store == nil {
		return
	}
	store.mutex.Lock()
	store.states = make(map[string]FileReadState)
	store.mutex.Unlock()
}
