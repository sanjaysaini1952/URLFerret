package main

import (
	"net/url"
	"regexp"
	"strings"
)

type scopeLevel int

const (
	scopeRDN  scopeLevel = iota
	scopeFQDN
	scopeDN
)

func parseFieldScope(s string) scopeLevel {
	switch strings.ToLower(s) {
	case "rdn":
		return scopeRDN
	case "fqdn":
		return scopeFQDN
	case "dn":
		return scopeDN
	default:
		return scopeRDN
	}
}

func isInFieldScope(rawURL, targetHost, fieldScope string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	host := parsed.Hostname()
	if host == "" {
		return true
	}

	switch parseFieldScope(fieldScope) {
	case scopeFQDN:
		return host == targetHost
	case scopeRDN:
		return matchesRootDomain(host, targetHost)
	case scopeDN:
		return matchesDomainKeyword(host, targetHost)
	}
	return true
}

func matchesRootDomain(host, targetHost string) bool {
	targetParts := strings.Split(targetHost, ".")
	if len(targetParts) < 2 {
		return strings.EqualFold(host, targetHost)
	}
	// Get root domain (last 2 parts for most domains, last 3 for co.uk etc)
	rootDomain := strings.Join(targetParts[max(0, len(targetParts)-2):], ".")
	return strings.HasSuffix(strings.ToLower(host), "."+strings.ToLower(rootDomain)) ||
		strings.EqualFold(host, targetHost)
}

func matchesDomainKeyword(host, targetHost string) bool {
	targetParts := strings.Split(targetHost, ".")
	if len(targetParts) < 2 {
		return strings.EqualFold(host, targetHost)
	}
	keyword := targetParts[len(targetParts)-2]
	return strings.Contains(strings.ToLower(host), strings.ToLower(keyword))
}

func isInCrawlScope(rawURL string, cfg *config) bool {
	if cfg.noScope {
		return true
	}
	if len(cfg.crawlScopeRegex) > 0 {
		for _, re := range cfg.crawlScopeRegex {
			if re.MatchString(rawURL) {
				return true
			}
		}
		return false
	}
	return true
}

func isOutOfScope(rawURL string, cfg *config) bool {
	if len(cfg.crawlOutScopeRegex) > 0 {
		for _, re := range cfg.crawlOutScopeRegex {
			if re.MatchString(rawURL) {
				return true
			}
		}
	}
	return false
}

func compileRegexList(patterns []string) []*regexp.Regexp {
	var res []*regexp.Regexp
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err == nil {
			res = append(res, re)
		}
	}
	return res
}

// extractFieldValue extracts a specific field from a URL
func extractFieldValue(rawURL, field string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	switch field {
	case "url":
		return rawURL
	case "path":
		return parsed.Path
	case "fqdn":
		return parsed.Hostname()
	case "rdn":
		host := parsed.Hostname()
		parts := strings.Split(host, ".")
		if len(parts) >= 2 {
			return strings.Join(parts[len(parts)-2:], ".")
		}
		return host
	case "rurl":
		return parsed.Scheme + "://" + parsed.Host
	case "qurl":
		if parsed.RawQuery != "" {
			return rawURL
		}
		return parsed.Scheme + "://" + parsed.Host + parsed.Path + "?" + parsed.RawQuery
	case "qpath":
		if parsed.RawQuery != "" {
			return parsed.Path + "?" + parsed.RawQuery
		}
		return parsed.Path
	case "file":
		parts := strings.Split(parsed.Path, "/")
		return parts[len(parts)-1]
	case "ufile":
		parts := strings.Split(parsed.Path, "/")
		filename := parts[len(parts)-1]
		if filename == "" {
			return ""
		}
		return parsed.Scheme + "://" + parsed.Host + parsed.Path
	case "key":
		keys := make([]string, 0)
		for k := range parsed.Query() {
			keys = append(keys, k)
		}
		return strings.Join(keys, ",")
	case "value":
		vals := make([]string, 0)
		for _, v := range parsed.Query() {
			vals = append(vals, v...)
		}
		return strings.Join(vals, ",")
	case "kv":
		if parsed.RawQuery != "" {
			return parsed.RawQuery
		}
		return ""
	case "dir":
		path := parsed.Path
		if idx := strings.LastIndex(path, "/"); idx != -1 {
			return path[:idx+1]
		}
		return "/"
	case "udir":
		path := parsed.Path
		if idx := strings.LastIndex(path, "/"); idx != -1 {
			return parsed.Scheme + "://" + parsed.Host + path[:idx+1]
		}
		return parsed.Scheme + "://" + parsed.Host + "/"
	}
	return rawURL
}
