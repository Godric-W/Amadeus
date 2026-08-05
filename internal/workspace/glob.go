package workspace

import (
	"errors"
	"path"
	"strings"
)

func NormalizeGlob(pattern string) (string, error) {
	pattern = strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/"))
	if pattern == "" {
		return "", errors.New("glob pattern is empty")
	}
	if path.IsAbs(pattern) {
		return "", errors.New("glob pattern must be relative")
	}
	for _, segment := range strings.Split(pattern, "/") {
		if segment == ".." {
			return "", errors.New("glob pattern cannot escape its root")
		}
	}
	if _, err := path.Match(strings.ReplaceAll(pattern, "**", "*"), "validation"); err != nil {
		return "", err
	}
	return pattern, nil
}

func MatchGlob(pattern, candidate string) (bool, error) {
	patternSegments := strings.Split(strings.TrimPrefix(pattern, "./"), "/")
	candidateSegments := strings.Split(strings.TrimPrefix(candidate, "./"), "/")
	type position struct{ pattern, candidate int }
	memo := make(map[position]bool)
	seen := make(map[position]bool)
	var match func(int, int) (bool, error)
	match = func(patternIndex, candidateIndex int) (bool, error) {
		key := position{patternIndex, candidateIndex}
		if seen[key] {
			return memo[key], nil
		}
		seen[key] = true
		if patternIndex == len(patternSegments) {
			memo[key] = candidateIndex == len(candidateSegments)
			return memo[key], nil
		}
		if patternSegments[patternIndex] == "**" {
			zero, err := match(patternIndex+1, candidateIndex)
			if err != nil || zero {
				memo[key] = zero
				return zero, err
			}
			if candidateIndex < len(candidateSegments) {
				more, err := match(patternIndex, candidateIndex+1)
				memo[key] = more
				return more, err
			}
			return false, nil
		}
		if candidateIndex >= len(candidateSegments) {
			return false, nil
		}
		segmentMatch, err := path.Match(patternSegments[patternIndex], candidateSegments[candidateIndex])
		if err != nil || !segmentMatch {
			return false, err
		}
		matched, err := match(patternIndex+1, candidateIndex+1)
		memo[key] = matched
		return matched, err
	}
	return match(0, 0)
}
