package main

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var sitemapURLRe = regexp.MustCompile(`<loc>(.*?)</loc>`)
var sitemapIndexRe = regexp.MustCompile(`<sitemap>(.*?)</sitemap>`)

func fetchKnownFiles(baseURL string, types []string, timeout, retry int) []string {
	var results []string
	seen := make(map[string]bool)

	for _, t := range types {
		var urls []string
		switch strings.ToLower(t) {
		case "robotstxt", "robots":
			urls = append(urls, baseURL+"/robots.txt")
		case "sitemapxml", "sitemap":
			urls = append(urls, baseURL+"/sitemap.xml")
		case "all":
			urls = append(urls, baseURL+"/robots.txt", baseURL+"/sitemap.xml")
		}
		for _, u := range urls {
			if seen[u] {
				continue
			}
			seen[u] = true
			body, err := fetchWithRetry(u, timeout, retry)
			if err != nil || body == "" {
				continue
			}
			results = append(results, u)

			if strings.Contains(u, "sitemap") {
				for _, m := range sitemapURLRe.FindAllStringSubmatch(body, -1) {
					if len(m) > 1 && !seen[m[1]] {
						seen[m[1]] = true
						results = append(results, m[1])
					}
				}
				for _, m := range sitemapIndexRe.FindAllStringSubmatch(body, -1) {
					// If it's a sitemap index, fetch sub-sitemaps
					subURL := extractLocFromSitemap(m[0])
					if subURL != "" && !seen[subURL] {
						seen[subURL] = true
						subBody, err := fetchWithRetry(subURL, timeout, retry)
						if err == nil && subBody != "" {
							for _, sm := range sitemapURLRe.FindAllStringSubmatch(subBody, -1) {
								if len(sm) > 1 && !seen[sm[1]] {
									seen[sm[1]] = true
									results = append(results, sm[1])
								}
							}
						}
					}
				}
			}
		}
	}
	return results
}

func fetchWithRetry(url string, timeout, retry int) (string, error) {
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	for i := 0; i <= retry; i++ {
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}
		return string(body), nil
	}
	return "", nil
}

func extractLocFromSitemap(s string) string {
	re := regexp.MustCompile(`<loc>(.*?)</loc>`)
	m := re.FindStringSubmatch(s)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}
