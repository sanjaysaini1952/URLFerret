package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/corpix/uarand"
	"github.com/gocolly/colly/v2"
	"github.com/spf13/pflag"

	"urlferret/banner"
)

var seen sync.Map

type paramSet struct {
	mu     sync.Mutex
	params map[string]struct{}
}

var paramCache sync.Map

type Result struct {
	URL        string `json:"url"`
	Source     string `json:"source,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
}

type config struct {
	concurrency      int
	parallelism      int
	depth            int
	insecure         bool
	includeSubs      bool
	proxyURL         *url.URL
	maxtime          int
	disableRedirects bool
	timeout          int
	silent           bool
	verbose          bool
	outputFile       string
	includeExts      []string
	excludeExts      []string
	matchPattern     *regexp.Regexp
	filterPattern    *regexp.Regexp
	delay            int
	maxPages         int
	headers          map[string]string
	cookie           string
	extractEmails    bool
	extractComments  bool
	jsonOutput       bool
	statusCodes      map[int]bool
	retry            int
	noRobots         bool
}

func main() {
	cfg := config{}

	pflag.IntVarP(&cfg.concurrency, "concurrency", "c", 10, "Number of concurrent goroutines per domain")
	pflag.IntVarP(&cfg.parallelism, "parallelism", "p", 10, "Number of concurrent inputs to process")
	pflag.IntVar(&cfg.depth, "depth", 3, "Crawl depth")
	pflag.BoolVar(&cfg.insecure, "insecure", false, "Disable TLS verification")
	pflag.BoolVar(&cfg.includeSubs, "include-subdomains", false, "Include subdomains for crawling")
	proxy := pflag.String("proxy", "", "Proxy URL (e.g. http://127.0.0.1:8080)")
	pflag.IntVar(&cfg.maxtime, "maxtime", -1, "Max crawl time per URL in seconds")
	pflag.BoolVar(&cfg.disableRedirects, "disable-redirects", false, "Disable following redirects")
	pflag.IntVar(&cfg.timeout, "timeout", 30, "HTTP request timeout in seconds")
	pflag.BoolVar(&cfg.silent, "silent", false, "Silent mode (no banner)")
	pflag.BoolVar(&cfg.verbose, "verbose", false, "Enable verbose mode")
	version := pflag.Bool("version", false, "Print version and exit")
	pflag.StringVarP(&cfg.outputFile, "output", "o", "", "Save results to file")
	includeExts := pflag.StringSlice("include-ext", nil, "Only include URLs with these extensions (e.g. js,php)")
	excludeExts := pflag.StringSlice("exclude-ext", nil, "Exclude URLs with these extensions (e.g. png,css)")
	matchStr := pflag.String("match", "", "Regex pattern to include matching URLs")
	filterStr := pflag.String("filter", "", "Regex pattern to exclude matching URLs")
	pflag.IntVar(&cfg.delay, "delay", 0, "Delay between requests in milliseconds")
	pflag.IntVar(&cfg.maxPages, "max-pages", 0, "Max pages to crawl per domain (0 = unlimited)")
	headers := pflag.StringSlice("header", nil, "Custom headers (e.g. 'Authorization: Bearer xxx')")
	pflag.StringVar(&cfg.cookie, "cookie", "", "Cookie string to include with requests")
	pflag.BoolVar(&cfg.extractEmails, "emails", false, "Extract email addresses from pages")
	pflag.BoolVar(&cfg.extractComments, "comments", false, "Extract HTML comments from pages")
	pflag.BoolVarP(&cfg.jsonOutput, "json", "j", false, "Output as JSON (lines)")
	statusCodes := pflag.IntSlice("status-code", nil, "Only include URLs with these status codes")
	pflag.IntVar(&cfg.retry, "retry", 0, "Retry attempts on failure")
	pflag.BoolVar(&cfg.noRobots, "no-robots", false, "Ignore robots.txt")

	pflag.Parse()

	if *version {
		banner.PrintBanner()
		banner.PrintVersion()
		os.Exit(0)
	}

	if !cfg.silent {
		banner.PrintBanner()
	}

	if *proxy != "" {
		os.Setenv("PROXY", *proxy)
	}
	if envProxy := os.Getenv("PROXY"); envProxy != "" {
		cfg.proxyURL, _ = url.Parse(envProxy)
	}

	if *includeExts != nil {
		cfg.includeExts = *includeExts
	}
	if *excludeExts != nil {
		cfg.excludeExts = *excludeExts
	}
	if *matchStr != "" {
		cfg.matchPattern = regexp.MustCompile(*matchStr)
	}
	if *filterStr != "" {
		cfg.filterPattern = regexp.MustCompile(*filterStr)
	}
	if *headers != nil {
		cfg.headers = make(map[string]string)
		for _, h := range *headers {
			if parts := strings.SplitN(h, ":", 2); len(parts) == 2 {
				cfg.headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}
	if *statusCodes != nil {
		cfg.statusCodes = make(map[int]bool)
		for _, sc := range *statusCodes {
			cfg.statusCodes[sc] = true
		}
	}

	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		fmt.Fprintln(os.Stderr, "No input detected. Usage: cat urls.txt | urlferret")
		os.Exit(1)
	}

	results := make(chan Result, cfg.concurrency)
	jobs := make(chan string)

	var wg sync.WaitGroup
	for i := 0; i < cfg.parallelism; i++ {
		wg.Add(1)
		go worker(jobs, results, &cfg, &wg)
	}

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			jobs <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintln(os.Stderr, "stdin error:", err)
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var writer *bufio.Writer
	var file *os.File
	if cfg.outputFile != "" {
		var err error
		file, err = os.Create(cfg.outputFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Failed to create output file:", err)
			os.Exit(1)
		}
		defer file.Close()
		writer = bufio.NewWriter(file)
	} else {
		writer = bufio.NewWriter(os.Stdout)
	}
	defer writer.Flush()

	jsonEnc := json.NewEncoder(writer)
	for res := range results {
		if !isUnique(res.URL) {
			continue
		}
		if !matchByExtension(res.URL, cfg.includeExts, cfg.excludeExts) {
			continue
		}
		if cfg.matchPattern != nil && !cfg.matchPattern.MatchString(res.URL) {
			continue
		}
		if cfg.filterPattern != nil && cfg.filterPattern.MatchString(res.URL) {
			continue
		}
		if cfg.statusCodes != nil && !cfg.statusCodes[res.StatusCode] {
			continue
		}
		if cfg.jsonOutput {
			jsonEnc.Encode(res)
		} else {
			fmt.Fprintln(writer, res.URL)
		}
	}
}

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
		hostname, err := extractHostname(target)
		if err != nil {
			continue
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

		if cfg.includeSubs {
			c.AllowedDomains = nil
			pattern := `.*(\.|\/\/)` + strings.ReplaceAll(hostname, ".", `\.`) + `((#|\/|\?).*)?`
			c.URLFilters = []*regexp.Regexp{regexp.MustCompile(pattern)}
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
			if cfg.extractEmails || cfg.extractComments {
				bodyBuf = r.Body
			}
		})

		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			link := e.Attr("href")
			resultURL := e.Request.AbsoluteURL(link)
			if resultURL != "" {
				safeSendResult(results, Result{URL: resultURL, Source: "link", StatusCode: statusCode})
			}
			e.Request.Visit(link)
		})

		c.OnHTML("script[src]", func(e *colly.HTMLElement) {
			resultURL := e.Request.AbsoluteURL(e.Attr("src"))
			if resultURL != "" {
				safeSendResult(results, Result{URL: resultURL, Source: "script", StatusCode: statusCode})
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
			if !ok {
				return
			}
			ps := val.(*paramSet)
			ps.mu.Lock()
			names := make([]string, 0, len(ps.params))
			for name := range ps.params {
				names = append(names, name+"=rix4uni")
			}
			ps.mu.Unlock()

			if len(names) == 0 {
				return
			}

			parsed, err := url.Parse(urlStr)
			if err != nil {
				return
			}
			parsed.RawQuery = strings.Join(names, "&")
			resultURL := parsed.String()
			if resultURL != urlStr {
				safeSendResult(results, Result{URL: resultURL, Source: "param", StatusCode: statusCode})
			}

			if cfg.extractEmails && len(bodyBuf) > 0 {
				matches := emailRegex.FindAllString(string(bodyBuf), -1)
				for _, m := range matches {
					safeSendResult(results, Result{URL: m, Source: "email", StatusCode: statusCode})
				}
			}

			if cfg.extractComments && len(bodyBuf) > 0 {
				matches := commentRegex.FindAllString(string(bodyBuf), -1)
				for _, m := range matches {
					trimmed := strings.TrimSpace(m)
					if trimmed != "" {
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

func safeSendResult(results chan<- Result, res Result) {
	defer func() {
		recover()
	}()
	results <- res
}

func extractHostname(raw string) (string, error) {
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", errors.New("invalid URL or domain")
	}
	return u.Hostname(), nil
}

func isUnique(s string) bool {
	_, loaded := seen.LoadOrStore(s, true)
	return !loaded
}

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
