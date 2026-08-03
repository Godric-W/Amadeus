package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/project"
)

const (
	defaultMaxFiles = 4096
	defaultMaxBytes = 128 << 20
)

var ErrSnapshotNotFound = errors.New("snapshot not found")

type ChangeKind string

const (
	ChangeAdded    ChangeKind = "added"
	ChangeModified ChangeKind = "modified"
	ChangeDeleted  ChangeKind = "deleted"
)

type FileChange struct {
	Path string     `json:"path"`
	Kind ChangeKind `json:"kind"`
}

type Snapshot struct {
	RunID     string       `json:"run_id"`
	CreatedAt time.Time    `json:"created_at"`
	Completed time.Time    `json:"completed_at,omitempty"`
	Changes   []FileChange `json:"changes,omitempty"`
}

type RevertResult struct {
	RunID   string       `json:"run_id"`
	Changes []FileChange `json:"changes,omitempty"`
}

type Service interface {
	Begin(context.Context, string) (Snapshot, error)
	Complete(context.Context, string) (Snapshot, error)
	Revert(context.Context, string) (RevertResult, error)
}

type FileServiceOptions struct {
	StoragePath string
	MaxFiles    int
	MaxBytes    int64
	Now         func() time.Time
}

type FileService struct {
	root     project.Root
	storage  string
	maxFiles int
	maxBytes int64
	now      func() time.Time
}

type manifest struct {
	RunID     string       `json:"run_id"`
	CreatedAt time.Time    `json:"created_at"`
	Files     []fileRecord `json:"files"`
}

type fileRecord struct {
	Path string      `json:"path"`
	Mode fs.FileMode `json:"mode"`
	Size int64       `json:"size"`
	Hash string      `json:"sha256"`
}

func NewFileService(root project.Root, options FileServiceOptions) (*FileService, error) {
	if root.Path() == "" {
		return nil, errors.New("snapshot project root is empty")
	}
	if options.MaxFiles <= 0 {
		options.MaxFiles = defaultMaxFiles
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = defaultMaxBytes
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	storage := options.StoragePath
	if strings.TrimSpace(storage) == "" {
		storage = filepath.Join(root.Path(), ".amadeus", "snapshots")
	}
	absStorage, err := filepath.Abs(storage)
	if err != nil {
		return nil, fmt.Errorf("resolve snapshot storage: %w", err)
	}
	if _, err := root.Relative(absStorage); err != nil {
		return nil, fmt.Errorf("snapshot storage must be inside project root: %w", err)
	}
	return &FileService{root: root, storage: filepath.Clean(absStorage), maxFiles: options.MaxFiles, maxBytes: options.MaxBytes, now: options.Now}, nil
}

func (service *FileService) Begin(ctx context.Context, runID string) (Snapshot, error) {
	if err := service.validate(ctx, runID); err != nil {
		return Snapshot{}, err
	}
	directory := service.snapshotDirectory(runID)
	if _, err := os.Stat(directory); err == nil {
		return Snapshot{}, fmt.Errorf("snapshot already exists for run %q", runID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, fmt.Errorf("inspect snapshot directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "before"), 0o700); err != nil {
		return Snapshot{}, fmt.Errorf("create snapshot directory: %w", err)
	}
	createdAt := service.now().UTC()
	files, err := service.capture(ctx, filepath.Join(directory, "before"), true)
	if err != nil {
		return Snapshot{}, err
	}
	if err := writeJSON(filepath.Join(directory, "before.json"), manifest{RunID: runID, CreatedAt: createdAt, Files: files}); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{RunID: runID, CreatedAt: createdAt}, nil
}

func (service *FileService) Complete(ctx context.Context, runID string) (Snapshot, error) {
	if err := service.validate(ctx, runID); err != nil {
		return Snapshot{}, err
	}
	directory := service.snapshotDirectory(runID)
	before, err := readManifest(filepath.Join(directory, "before.json"))
	if err != nil {
		return Snapshot{}, err
	}
	afterFiles, err := service.capture(ctx, "", false)
	if err != nil {
		return Snapshot{}, err
	}
	completedAt := service.now().UTC()
	after := manifest{RunID: runID, CreatedAt: completedAt, Files: afterFiles}
	if err := writeJSON(filepath.Join(directory, "after.json"), after); err != nil {
		return Snapshot{}, err
	}
	changes := diff(before.Files, after.Files)
	if err := writeJSON(filepath.Join(directory, "changes.json"), changes); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{RunID: runID, CreatedAt: before.CreatedAt, Completed: completedAt, Changes: changes}, nil
}

func (service *FileService) Revert(ctx context.Context, runID string) (RevertResult, error) {
	if err := service.validate(ctx, runID); err != nil {
		return RevertResult{}, err
	}
	directory := service.snapshotDirectory(runID)
	before, err := readManifest(filepath.Join(directory, "before.json"))
	if err != nil {
		return RevertResult{}, err
	}
	current, err := service.capture(ctx, "", false)
	if err != nil {
		return RevertResult{}, err
	}
	changes := diff(before.Files, current)
	beforeByPath := indexFiles(before.Files)
	currentByPath := indexFiles(current)
	for path := range currentByPath {
		if _, exists := beforeByPath[path]; exists {
			continue
		}
		if err := os.Remove(filepath.Join(service.root.Path(), filepath.FromSlash(path))); err != nil && !errors.Is(err, os.ErrNotExist) {
			return RevertResult{}, fmt.Errorf("remove file added after snapshot %q: %w", path, err)
		}
	}
	for _, file := range before.Files {
		if err := ctx.Err(); err != nil {
			return RevertResult{}, err
		}
		content, err := os.ReadFile(filepath.Join(directory, "before", filepath.FromSlash(file.Path)))
		if err != nil {
			return RevertResult{}, fmt.Errorf("read snapshot file %q: %w", file.Path, err)
		}
		destination := filepath.Join(service.root.Path(), filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return RevertResult{}, fmt.Errorf("create restore parent for %q: %w", file.Path, err)
		}
		if err := os.WriteFile(destination, content, file.Mode.Perm()); err != nil {
			return RevertResult{}, fmt.Errorf("restore snapshot file %q: %w", file.Path, err)
		}
	}
	return RevertResult{RunID: runID, Changes: changes}, nil
}

func (service *FileService) capture(ctx context.Context, copyRoot string, copyFiles bool) ([]fileRecord, error) {
	files := make([]fileRecord, 0)
	var totalBytes int64
	err := filepath.WalkDir(service.root.Path(), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := service.root.Relative(path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if entry.IsDir() {
			if relative == ".git" || relative == ".amadeus/snapshots" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if len(files) >= service.maxFiles {
			return fmt.Errorf("snapshot exceeds maximum file count %d", service.maxFiles)
		}
		if info.Size() > service.maxBytes-totalBytes {
			return fmt.Errorf("snapshot exceeds maximum byte size %d", service.maxBytes)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		totalBytes += int64(len(content))
		hash := sha256.Sum256(content)
		record := fileRecord{Path: filepath.ToSlash(relative), Mode: info.Mode(), Size: int64(len(content)), Hash: hex.EncodeToString(hash[:])}
		files = append(files, record)
		if copyFiles {
			destination := filepath.Join(copyRoot, filepath.FromSlash(record.Path))
			if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(destination, content, info.Mode().Perm()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("capture project snapshot: %w", err)
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Path < files[right].Path })
	return files, nil
}

func (service *FileService) validate(ctx context.Context, runID string) error {
	if service == nil || service.root.Path() == "" {
		return errors.New("snapshot service is nil")
	}
	if ctx == nil {
		return errors.New("snapshot context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validRunID(runID) {
		return fmt.Errorf("snapshot run ID %q is invalid", runID)
	}
	return nil
}

func (service *FileService) snapshotDirectory(runID string) string {
	return filepath.Join(service.storage, runID)
}

func validRunID(value string) bool {
	if strings.TrimSpace(value) == "" || len(value) > 160 {
		return false
	}
	for _, runeValue := range value {
		if runeValue >= 'a' && runeValue <= 'z' || runeValue >= 'A' && runeValue <= 'Z' || runeValue >= '0' && runeValue <= '9' || runeValue == '-' || runeValue == '_' || runeValue == '.' {
			continue
		}
		return false
	}
	return true
}

func writeJSON(path string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode snapshot metadata: %w", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("write snapshot metadata: %w", err)
	}
	return nil
}

func readManifest(path string) (manifest, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return manifest{}, fmt.Errorf("%w: %s", ErrSnapshotNotFound, filepath.Base(filepath.Dir(path)))
	}
	if err != nil {
		return manifest{}, fmt.Errorf("read snapshot metadata: %w", err)
	}
	var decoded manifest
	if err := json.Unmarshal(content, &decoded); err != nil {
		return manifest{}, fmt.Errorf("decode snapshot metadata: %w", err)
	}
	if !validRunID(decoded.RunID) {
		return manifest{}, errors.New("snapshot metadata has invalid run ID")
	}
	return decoded, nil
}

func diff(before, after []fileRecord) []FileChange {
	beforeByPath := indexFiles(before)
	afterByPath := indexFiles(after)
	changes := make([]FileChange, 0)
	for path, oldFile := range beforeByPath {
		newFile, exists := afterByPath[path]
		if !exists {
			changes = append(changes, FileChange{Path: path, Kind: ChangeDeleted})
			continue
		}
		if oldFile.Hash != newFile.Hash || oldFile.Mode.Perm() != newFile.Mode.Perm() {
			changes = append(changes, FileChange{Path: path, Kind: ChangeModified})
		}
	}
	for path := range afterByPath {
		if _, exists := beforeByPath[path]; !exists {
			changes = append(changes, FileChange{Path: path, Kind: ChangeAdded})
		}
	}
	sort.Slice(changes, func(left, right int) bool { return changes[left].Path < changes[right].Path })
	return changes
}

func indexFiles(files []fileRecord) map[string]fileRecord {
	indexed := make(map[string]fileRecord, len(files))
	for _, file := range files {
		indexed[file.Path] = file
	}
	return indexed
}

var _ Service = (*FileService)(nil)
