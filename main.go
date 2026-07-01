package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/spf13/pflag"

	"urlferret/banner"
)

var seen sync.Map
var globalStartTime time.Time

type paramSet struct {
	mu     sync.Mutex
	params map[string]struct{}
}

type Result struct {
	URL         string `json:"url"`
	Source      string `json:"source,omitempty"`
	StatusCode  int    `json:"status_code,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Technologies string `json:"technologies,omitempty"`
}

func main() {
	globalStartTime = time.Now()
	cfg := &config{}

	// Original flags
	pflag.IntVarP(&cfg.concurrency, "concurrency", "c", 10, "Number of concurrent goroutines per domain")
	pflag.IntVarP(&cfg.parallelism, "parallelism", "p", 10, "Number of concurrent inputs to process")
	pflag.IntVar(&cfg.depth, "depth", 3, "Maximum depth to crawl")
	pflag.BoolVar(&cfg.insecure, "insecure", false, "Disable TLS verification")
	pflag.BoolVar(&cfg.includeSubs, "include-subdomains", false, "Include subdomains for crawling")
	proxy := pflag.String("proxy", "", "Proxy URL (e.g. http://127.0.0.1:8080)")
	pflag.IntVar(&cfg.maxtime, "maxtime", -1, "Max crawl time per URL in seconds")
	pflag.BoolVar(&cfg.disableRedirects, "disable-redirects", false, "Disable following redirects")
	pflag.IntVar(&cfg.timeout, "timeout", 10, "HTTP request timeout in seconds")
	pflag.BoolVar(&cfg.silent, "silent", false, "Silent mode (no banner)")
	pflag.BoolVar(&cfg.verbose, "verbose", false, "Enable verbose mode")
	version := pflag.Bool("version", false, "Print version and exit")
	pflag.StringVarP(&cfg.outputFile, "output", "o", "", "File to write output to")
	includeExts := pflag.StringSlice("include-ext", nil, "Match output for given extensions (e.g. js,php)")
	excludeExts := pflag.StringSlice("exclude-ext", nil, "Filter output for given extensions (e.g. png,css)")
	matchStr := pflag.String("match", "", "Regex to match on output URL")
	filterStr := pflag.String("filter", "", "Regex to filter on output URL")
	pflag.IntVar(&cfg.delay, "delay", 0, "Delay between requests in milliseconds")
	pflag.IntVar(&cfg.maxPages, "max-pages", 0, "Max pages to crawl per domain (0 = unlimited)")
	headers := pflag.StringSlice("header", nil, "Custom headers (e.g. 'Authorization: Bearer xxx')")
	pflag.StringVar(&cfg.cookie, "cookie", "", "Cookie string to include with requests")
	pflag.BoolVar(&cfg.extractEmails, "emails", false, "Extract email addresses from pages")
	pflag.BoolVar(&cfg.extractComments, "comments", false, "Extract HTML comments from pages")
	pflag.BoolVarP(&cfg.jsonOutput, "json", "j", false, "Write output in JSON format")
	statusCodes := pflag.IntSlice("status-code", nil, "Only include URLs with these status codes")
	pflag.IntVar(&cfg.retry, "retry", 1, "Number of times to retry the request")
	pflag.BoolVar(&cfg.noRobots, "no-robots", false, "Ignore robots.txt")

	// Katana-like flags
	pflag.BoolVar(&cfg.jsCrawl, "js-crawl", false, "Enable endpoint parsing in JavaScript files")
	pflag.BoolVar(&cfg.jsluice, "jsluice", false, "Enable jsluice parsing in JS files (memory intensive)")
	pflag.IntVar(&cfg.crawlDuration, "crawl-duration", 0, "Maximum duration to crawl for (seconds)")
	pflag.StringVar(&cfg.knownFiles, "known-files", "", "Crawl known files (all,robotstxt,sitemapxml)")
	pflag.BoolVar(&cfg.automaticFormFill, "automatic-form-fill", false, "Enable automatic form filling (experimental)")
	pflag.BoolVar(&cfg.formExtraction, "form-extraction", false, "Extract form, input, textarea elements")
	pflag.BoolVar(&cfg.techDetect, "tech-detect", false, "Enable technology detection")
	pflag.StringVar(&cfg.strategy, "strategy", "depth-first", "Visit strategy (depth-first, breadth-first)")
	pflag.BoolVar(&cfg.ignoreQueryParams, "ignore-query-params", false, "Ignore crawling same path with different query params")
	pflag.BoolVar(&cfg.filterSimilar, "filter-similar", false, "Filter crawling of similar looking URLs")
	pflag.IntVar(&cfg.filterSimilarThreshold, "filter-similar-threshold", 10, "Threshold for similar URL filtering")
	pflag.BoolVar(&cfg.tlsImpersonate, "tls-impersonate", false, "Enable TLS client hello randomization")
	pflag.StringVar(&cfg.fieldScope, "field-scope", "", "Scope field (rdn, fqdn, dn) or custom regex")
	crawlScope := pflag.StringSlice("crawl-scope", nil, "In-scope URL regex patterns")
	crawlOutScope := pflag.StringSlice("crawl-out-scope", nil, "Out-of-scope URL regex patterns")
	pflag.BoolVar(&cfg.noScope, "no-scope", false, "Disable host-based default scope")
	pflag.BoolVar(&cfg.displayOutScope, "display-out-scope", false, "Display out-of-scope endpoints")
	pflag.StringVar(&cfg.outputField, "field", "", "Output field (url,path,fqdn,rdn,rurl,qurl,file,key,value,kv,dir)")
	pflag.StringVar(&cfg.storeField, "store-field", "", "Store field to per-host files")
	pflag.StringVar(&cfg.storeFieldDir, "store-field-dir", "", "Directory for stored fields")
	pflag.BoolVar(&cfg.storeResponse, "store-response", false, "Store HTTP responses to disk")
	pflag.StringVar(&cfg.storeResponseDir, "store-response-dir", "responses", "Directory for stored responses")
	pflag.StringVar(&cfg.resume, "resume", "", "Resume scan using resume file")
	pflag.IntVar(&cfg.rateLimit, "rate-limit", 0, "Max requests per second")
	pflag.IntVar(&cfg.rateLimitMinute, "rate-limit-minute", 0, "Max requests per minute")
	pflag.IntVar(&cfg.hostRateLimit, "host-rate-limit", 0, "Max requests per second per host")
	pflag.IntVar(&cfg.hostRateLimitMinute, "host-rate-limit-minute", 0, "Max requests per minute per host")
	resolvers := pflag.StringSlice("resolvers", nil, "Custom resolvers")
	pflag.IntVar(&cfg.maxResponseSize, "max-response-size", 4194304, "Max response size to read")
	pflag.IntVar(&cfg.maxDomainPages, "max-domain-pages", 0, "Max pages per domain (0 = unlimited)")

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
	if *crawlScope != nil {
		cfg.crawlScope = *crawlScope
		cfg.crawlScopeRegex = compileRegexList(cfg.crawlScope)
	}
	if *crawlOutScope != nil {
		cfg.crawlOutScope = *crawlOutScope
		cfg.crawlOutScopeRegex = compileRegexList(cfg.crawlOutScope)
	}
	if *resolvers != nil {
		cfg.resolvers = *resolvers
	}

	if cfg.jsCrawl || cfg.jsluice {
		cfg.noRobots = true
	}

	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		if cfg.resume == "" {
			fmt.Fprintln(os.Stderr, "No input detected. Usage: cat urls.txt | urlferret")
			os.Exit(1)
		}
	}

	results := make(chan Result, cfg.concurrency)
	jobs := make(chan string)

	var wg sync.WaitGroup
	for i := 0; i < cfg.parallelism; i++ {
		wg.Add(1)
		go worker(jobs, results, cfg, &wg)
	}

	go func() {
		if cfg.resume != "" {
			loadResumeFile(cfg.resume, jobs)
		}
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			jobs <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintln(os.Stderr, "stdin error:", err)
		}
		close(jobs)
	}()

	// Monitor crawl duration
	if cfg.crawlDuration > 0 {
		go func() {
			time.Sleep(time.Duration(cfg.crawlDuration) * time.Second)
			if cfg.verbose {
				log.Printf("[duration] crawl duration of %ds reached, stopping", cfg.crawlDuration)
			}
			os.Exit(0)
		}()
	}

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
		if cfg.filterSimilar && isSimilarURL(res.URL, cfg.filterSimilarThreshold) {
			continue
		}

		outputURL := res.URL
		if cfg.outputField != "" {
			outputURL = extractFieldValue(res.URL, cfg.outputField)
		}

		if cfg.storeField != "" {
			storeFieldToDisk(res, cfg.storeField, cfg.storeFieldDir)
		}

		if cfg.jsonOutput {
			res.URL = outputURL
			jsonEnc.Encode(res)
		} else {
			fmt.Fprintln(writer, outputURL)
		}
	}
}

func storeFieldToDisk(res Result, field, dir string) {
	if dir == "" {
		dir = "urlferret_fields"
	}
	os.MkdirAll(dir, 0755)
	hostname, _ := url.Parse(res.URL)
	if hostname == nil {
		return
	}
	safeHost := strings.ReplaceAll(hostname.Hostname(), ":", "_")
	filename := fmt.Sprintf("%s_%s.txt", safeHost, field)
	filePath := filepath.Join(dir, filename)
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	val := extractFieldValue(res.URL, field)
	if val != "" {
		fmt.Fprintln(f, val)
	}
}

func isUnique(s string) bool {
	_, loaded := seen.LoadOrStore(s, true)
	return !loaded
}

func loadResumeFile(path string, jobs chan<- string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		jobs <- scanner.Text()
	}
}


