package main

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	jsURLRe  = regexp.MustCompile(`["'` + "`" + `](https?://[^"'\s` + "`" + `]+)["'` + "`" + `]`)
	jsPathRe = regexp.MustCompile(`["'` + "`" + `](/[^"'\s?` + "`" + `]+)["'` + "`" + `]`)
	jsAPIRe  = regexp.MustCompile(`["'` + "`" + `](/api/[^"'\s` + "`" + `]*)["'` + "`" + `]`)
	jsCallRe = regexp.MustCompile(`(fetch|axios|ajax|XMLHttpRequest|xhr)\s*\(\s*["'` + "`" + `]([^"'\s` + "`" + `]+)["'` + "`" + `]`)
)

func parseJSContent(baseURL, content string) []string {
	var endpoints []string
	seen := make(map[string]bool)

	for _, m := range jsURLRe.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 && !seen[m[1]] {
			seen[m[1]] = true
			endpoints = append(endpoints, m[1])
		}
	}
	for _, m := range jsPathRe.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 && !seen[m[1]] {
			seen[m[1]] = true
			if r := resolveJSURL(baseURL, m[1]); r != "" {
				endpoints = append(endpoints, r)
			}
		}
	}
	for _, m := range jsAPIRe.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 && !seen[m[1]] {
			seen[m[1]] = true
			if r := resolveJSURL(baseURL, m[1]); r != "" {
				endpoints = append(endpoints, r)
			}
		}
	}
	for _, m := range jsCallRe.FindAllStringSubmatch(content, -1) {
		if len(m) > 2 {
			u := strings.TrimSpace(m[2])
			if !seen[u] {
				seen[u] = true
				if r := resolveJSURL(baseURL, u); r != "" {
					endpoints = append(endpoints, r)
				}
			}
		}
	}
	return endpoints
}

func fetchJS(jsURL string, timeout int) (string, error) {
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	req, _ := http.NewRequest("GET", jsURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func resolveJSURL(base, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	r, err := b.Parse(path)
	if err != nil {
		return ""
	}
	return r.String()
}
