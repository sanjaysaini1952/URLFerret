package main

import (
	"net/url"
	"regexp"
)

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

	// Katana-like features
	jsCrawl            bool
	jsluice            bool
	crawlDuration      int
	knownFiles         string
	automaticFormFill  bool
	formExtraction     bool
	techDetect         bool
	strategy           string
	ignoreQueryParams  bool
	filterSimilar      bool
	filterSimilarThreshold int
	tlsImpersonate     bool
	fieldScope         string
	crawlScope         []string
	crawlOutScope      []string
	noScope            bool
	displayOutScope    bool
	outputField        string
	storeField         string
	storeFieldDir      string
	storeResponse      bool
	storeResponseDir   string
	resume             string
	rateLimit          int
	rateLimitMinute    int
	hostRateLimit      int
	hostRateLimitMinute int
	resolvers          []string
	maxResponseSize    int
	maxDomainPages     int

	// Compiled regexes for scope
	crawlScopeRegex    []*regexp.Regexp
	crawlOutScopeRegex []*regexp.Regexp
}
