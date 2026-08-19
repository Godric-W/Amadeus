package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

func cloneResources(values []SkillResource) []SkillResource {
	return append([]SkillResource(nil), values...)
}

func discoverResources(root string) (map[ResourceKind][]SkillResource, error) {
	result := map[ResourceKind][]SkillResource{
		ResourceReference: nil,
		ResourceScript:    nil,
		ResourceAsset:     nil,
	}
	for directory, kind := range map[string]ResourceKind{"references": ResourceReference, "scripts": ResourceScript, "assets": ResourceAsset} {
		base := filepath.Join(root, directory)
		if _, err := os.Stat(base); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			digest, err := hashFile(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			result[kind] = append(result[kind], SkillResource{Path: filepath.ToSlash(relative), Kind: kind, Size: info.Size(), Revision: hex.EncodeToString(digest[:])})
			return nil
		})
		if err != nil {
			return nil, err
		}
		sort.Slice(result[kind], func(left, right int) bool { return result[kind][left].Path < result[kind][right].Path })
	}
	return result, nil
}

func hashFile(path string) ([sha256.Size]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return [sha256.Size]byte{}, err
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result, nil
}
