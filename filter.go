package main

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var similarFilter sync.Map

type pathPattern struct {
	mu     sync.Mutex
	values map[int]map[string]int // position -> value -> count
}

var pathTracker sync.Map

func matchByExtension(rawURL string, include, exclude []string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return len(include) == 0
	}
	path := parsed.Path
	lastDot := strings.LastIndex(path, ".")
	if lastDot == -1 {
		return len(include) == 0
	}
	ext := strings.ToLower(path[lastDot+1:])

	if len(exclude) > 0 {
		for _, e := range exclude {
			if strings.EqualFold(ext, e) {
				return false
			}
		}
	}
	if len(include) > 0 {
		for _, e := range include {
			if strings.EqualFold(ext, e) {
				return true
			}
		}
		return false
	}
	return true
}

// isSimilarURL checks if a URL is similar to already seen URLs
// by normalizing dynamic path segments (IDs, UUIDs, hashes, etc.)
func isSimilarURL(rawURL string, threshold int) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	path := parsed.Path
	segments := strings.Split(strings.Trim(path, "/"), "/")

	// Normalize the path by replacing dynamic segments
	normalized := make([]string, len(segments))
	for i, seg := range segments {
		if isDynamicSegment(seg) {
			normalized[i] = "{param}"

			// Track distinct values for this position
			patternKey := parsed.Host + parsed.Path
			val, _ := pathTracker.LoadOrStore(patternKey, &pathPattern{values: make(map[int]map[string]int)})
			pp := val.(*pathPattern)
			pp.mu.Lock()
			if pp.values[i] == nil {
				pp.values[i] = make(map[string]int)
			}
			pp.values[i][seg]++
			count := len(pp.values[i])
			pp.mu.Unlock()

			// If we've seen enough distinct values, this position is dynamic
			if count >= threshold {
				return true
			}
		} else {
			normalized[i] = seg
		}
	}

	normalizedPath := strings.Join(normalized, "/")
	if normalizedPath != path {
		_, loaded := similarFilter.LoadOrStore(parsed.Host+normalizedPath, true)
		return loaded
	}
	return false
}

func isDynamicSegment(seg string) bool {
	// Numbers
	if _, err := strconv.Atoi(seg); err == nil {
		return true
	}
	// UUID format
	if matched, _ := regexp.MatchString(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`, seg); matched {
		return true
	}
	// Hex hashes (32+ chars)
	if matched, _ := regexp.MatchString(`^[a-fA-F0-9]{32,}$`, seg); matched {
		return true
	}
	// Base64-like (long alphanumeric)
	if matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]{20,}$`, seg); matched {
		return true
	}
	// Dates (2024-01-01, 20240101)
	if matched, _ := regexp.MatchString(`^\d{4}[-/]?\d{2}[-/]?\d{2}$`, seg); matched {
		return true
	}
	return false
}

// hasSamePathWithDifferentParams checks if we've already crawled a URL with
// the same path but different query parameters
func hasSamePathWithDifferentParams(rawURL string) (string, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}

	if parsed.RawQuery == "" {
		return "", false
	}

	pathKey := parsed.Host + parsed.Path
	existing, loaded := similarFilter.LoadOrStore(pathKey, parsed.RawQuery)
	if loaded {
		return existing.(string), true
	}
	return "", false
}

func ignoreQueryParamKey(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parsed.RawQuery = ""
	return parsed.String()
}
