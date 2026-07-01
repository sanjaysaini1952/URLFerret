package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/corpix/uarand"
	"github.com/gocolly/colly/v2"
)

func worker(jobs <-chan string, results chan<- Result, cfg *config, wg *sync.WaitGroup) {
	defer wg.Done()

	emailRegex := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	commentRegex := regexp.MustCompile(`<!--(.*?)-->`)

	for input := range jobs {
		var urlsToTry []string
		if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
			urlsToTry = []string{input}
		} else {
			urlsToTry = []string{
				"https://" + input,
				"http://" + input,
				"https://www." + input,
				"http://www." + input,
			}
		}

		success := false
	urlLoop:
		for _, target := range urlsToTry {
			hostname, err := extractHostname2(target)
			if err != nil {
				continue
			}

			// Process known files (robots.txt, sitemap.xml) before crawling
			if cfg.knownFiles != "" {
				knownTypes := strings.Split(cfg.knownFiles, ",")
				baseURL := target
				if !strings.HasSuffix(baseURL, "/") {
					baseURL += "/"
				}
				knownResults := fetchKnownFiles(baseURL, knownTypes, cfg.timeout, cfg.retry)
				for _, kr := range knownResults {
					safeSendResult(results, Result{URL: kr, Source: "known", StatusCode: 200})
				}
			}

			allowedHosts := []string{hostname}
			if strings.HasPrefix(hostname, "www.") {
				allowedHosts = append(allowedHosts, strings.TrimPrefix(hostname, "www."))
			} else {
				allowedHosts = append(allowedHosts, "www."+hostname)
			}

			c := colly.NewCollector(
				colly.AllowedDomains(allowedHosts...),
				colly.MaxDepth(cfg.depth),
				colly.Async(true),
			)

			if cfg.noRobots {
				c.IgnoreRobotsTxt = true
			}

			c.UserAgent = uarand.GetRandom()

			// Scope control
			if cfg.noScope {
				c.AllowedDomains = nil
			}
			if cfg.includeSubs {
				c.AllowedDomains = nil
				pattern := `.*(\.|\/\/)` + strings.ReplaceAll(hostname, ".", `\.`) + `((#|\/|\?).*)?`
				c.URLFilters = []*regexp.Regexp{regexp.MustCompile(pattern)}
			}
			if cfg.fieldScope != "" {
				fs := parseFieldScope(cfg.fieldScope)
				if fs == scopeRDN {
					c.AllowedDomains = nil
					pattern := `.*(\.|\/\/)` + strings.ReplaceAll(extractRootDomain(hostname), ".", `\.`) + `((#|\/|\?).*)?`
					c.URLFilters = []*regexp.Regexp{regexp.MustCompile(pattern)}
				} else if fs == scopeFQDN {
					c.AllowedDomains = []string{hostname}
				}
			}
			if len(cfg.crawlScopeRegex) > 0 {
				c.URLFilters = cfg.crawlScopeRegex
			}

			if cfg.disableRedirects {
				c.SetRedirectHandler(func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				})
			}

			limitRule := &colly.LimitRule{DomainGlob: "*", Parallelism: cfg.concurrency}
			if cfg.delay > 0 {
				limitRule.Delay = time.Duration(cfg.delay) * time.Millisecond
			}
			c.Limit(limitRule)

			transport := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.insecure},
			}
			if cfg.proxyURL != nil && cfg.proxyURL.Host != "" {
				transport.Proxy = http.ProxyURL(cfg.proxyURL)
			}
			c.WithTransport(transport)

			c.SetRequestTimeout(time.Duration(cfg.timeout) * time.Second)

			if len(cfg.headers) > 0 {
				c.OnRequest(func(r *colly.Request) {
					for k, v := range cfg.headers {
						r.Headers.Set(k, v)
					}
				})
			}
			if cfg.cookie != "" {
				c.OnRequest(func(r *colly.Request) {
					r.Headers.Set("Cookie", cfg.cookie)
				})
			}

			statusCode := 0
			var bodyBuf []byte

			c.OnResponse(func(r *colly.Response) {
				statusCode = r.StatusCode
				if cfg.extractEmails || cfg.extractComments || cfg.jsCrawl {
					bodyBuf = r.Body
				}
				if cfg.storeResponse {
					storeResponseToDisk(r, cfg.storeResponseDir)
				}
			})

			c.OnHTML("a[href]", func(e *colly.HTMLElement) {
				link := e.Attr("href")
				resultURL := e.Request.AbsoluteURL(link)
				if resultURL == "" {
					return
				}
				if cfg.fieldScope != "" && !isInFieldScope(resultURL, hostname, cfg.fieldScope) {
					if cfg.displayOutScope {
						safeSendResult(results, Result{URL: resultURL, Source: "link:out-scope", StatusCode: statusCode})
					}
					return
				}
				if len(cfg.crawlOutScopeRegex) > 0 && isOutOfScope(resultURL, cfg) {
					if cfg.displayOutScope {
						safeSendResult(results, Result{URL: resultURL, Source: "link:out-scope", StatusCode: statusCode})
					}
					return
				}
				if cfg.ignoreQueryParams {
					if _, dup := hasSamePathWithDifferentParams(resultURL); dup {
						return
					}
				}
				safeSendResult(results, Result{URL: resultURL, Source: "link", StatusCode: statusCode})
				e.Request.Visit(link)
			})

			c.OnHTML("script[src]", func(e *colly.HTMLElement) {
				resultURL := e.Request.AbsoluteURL(e.Attr("src"))
				if resultURL == "" {
					return
				}
				safeSendResult(results, Result{URL: resultURL, Source: "script", StatusCode: statusCode})

				// JS crawling: fetch and parse JS files for endpoints
				if cfg.jsCrawl {
					go func(jsURL string) {
						content, err := fetchJS(jsURL, cfg.timeout)
						if err != nil {
							if cfg.verbose {
								log.Printf("JS fetch error %s: %v", jsURL, err)
							}
							return
						}
						endpoints := parseJSContent(jsURL, content)
						for _, ep := range endpoints {
							safeSendResult(results, Result{URL: ep, Source: "js_endpoint", StatusCode: 200})
						}
					}(resultURL)
				}
			})

			c.OnHTML("form[action]", func(e *colly.HTMLElement) {
				resultURL := e.Request.AbsoluteURL(e.Attr("action"))
				if resultURL != "" {
					safeSendResult(results, Result{URL: resultURL, Source: "form", StatusCode: statusCode})
				}
			})

			c.OnHTML("input[name], textarea[name]", func(e *colly.HTMLElement) {
				name := e.Attr("name")
				if name == "" {
					return
				}
				urlStr := e.Request.URL.String()
				val, _ := paramCache.LoadOrStore(urlStr, &paramSet{params: make(map[string]struct{})})
				ps := val.(*paramSet)
				ps.mu.Lock()
				ps.params[name] = struct{}{}
				ps.mu.Unlock()
			})

			c.OnScraped(func(r *colly.Response) {
				urlStr := r.Request.URL.String()
				val, ok := paramCache.LoadAndDelete(urlStr)
				if ok {
					ps := val.(*paramSet)
					ps.mu.Lock()
					names := make([]string, 0, len(ps.params))
					for name := range ps.params {
						names = append(names, name+"=rix4uni")
					}
					ps.mu.Unlock()

					if len(names) > 0 {
						parsed, err := url.Parse(urlStr)
						if err == nil {
							parsed.RawQuery = strings.Join(names, "&")
							resultURL := parsed.String()
							if resultURL != urlStr {
								safeSendResult(results, Result{URL: resultURL, Source: "param", StatusCode: statusCode})
							}
						}
					}
				}

				if cfg.extractEmails && len(bodyBuf) > 0 {
					for _, m := range emailRegex.FindAllString(string(bodyBuf), -1) {
						safeSendResult(results, Result{URL: m, Source: "email", StatusCode: statusCode})
					}
				}
				if cfg.extractComments && len(bodyBuf) > 0 {
					for _, m := range commentRegex.FindAllString(string(bodyBuf), -1) {
						if trimmed := strings.TrimSpace(m); trimmed != "" {
							safeSendResult(results, Result{URL: trimmed, Source: "comment", StatusCode: statusCode})
						}
					}
				}
			})

			if cfg.verbose {
				c.OnError(func(r *colly.Response, err error) {
					if r != nil && r.Request != nil {
						log.Println("Error on", r.Request.URL, ":", err)
					} else {
						log.Println("Error:", err)
					}
				})
			}

			doVisit := func(targetURL string) bool {
				for attempt := 0; attempt <= cfg.retry; attempt++ {
					if attempt > 0 && cfg.verbose {
						log.Printf("Retry %d/%d for %s", attempt, cfg.retry, targetURL)
					}
					statusCode = 0
					c.Visit(targetURL)
					c.Wait()
					if statusCode > 0 {
						return true
					}
				}
				return false
			}

			if cfg.maxtime == -1 {
				if doVisit(target) {
					success = true
					break
				}
			} else {
				done := make(chan struct{})
				var gotOK bool
				go func() {
					gotOK = doVisit(target)
					close(done)
				}()
				select {
				case <-done:
					if gotOK {
						success = true
						break urlLoop
					}
				case <-time.After(time.Duration(cfg.maxtime) * time.Second):
					if cfg.verbose {
						log.Println("[maxtime] reached for", target)
					}
					success = true
					break urlLoop
				}
			}
		}

		if !success && cfg.verbose {
			log.Println("Failed to crawl:", input)
		}
	}
}

func extractHostname2(raw string) (string, error) {
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("invalid URL: %s", raw)
	}
	return u.Hostname(), nil
}

func extractRootDomain(hostname string) string {
	parts := strings.Split(hostname, ".")
	if len(parts) < 2 {
		return hostname
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

func safeSendResult(results chan<- Result, res Result) {
	defer func() { recover() }()
	results <- res
}

func storeResponseToDisk(r *colly.Response, dir string) {
	if dir == "" {
		dir = "responses"
	}
	hostname := r.Request.URL.Hostname()
	path := r.Request.URL.Path
	if path == "" {
		path = "index"
	}
	safePath := strings.ReplaceAll(strings.Trim(path, "/"), "/", "_")
	if safePath == "" {
		safePath = "index"
	}
	filename := fmt.Sprintf("%s_%d_%s.txt", hostname, r.StatusCode, safePath)
	filePath := filepath.Join(dir, filename)
	os.MkdirAll(dir, 0755)
	os.WriteFile(filePath, r.Body, 0644)
}

var paramCache sync.Map
