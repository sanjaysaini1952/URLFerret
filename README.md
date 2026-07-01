# URLFerret

A fast web crawler for gathering URLs, JavaScript files, hidden parameters, email addresses, and HTML comments.

## Installation

### Go
```bash
go install github.com/sanjaysaini1952/URLFerret@latest
```

### Build from source
```bash
git clone https://github.com/sanjaysaini1952/URLFerret.git
cd URLFerret

# Linux
go build -o urlferret .

# Windows
go build -o urlferret.exe .

# Or use make
make build          # auto-detects OS
make build-linux
make build-windows
```

### Prebuilt binaries
Download from [releases](https://github.com/sanjaysaini1952/URLFerret/releases).

## Usage

```bash
cat urls.txt | urlferret [flags]
```

### Flags

| Flag | Description |
|---|---|
| `-c, --concurrency` | Concurrent goroutines per domain (default 10) |
| `-p, --parallelism` | Concurrent inputs to process (default 10) |
| `--depth` | Crawl depth (default 3) |
| `--insecure` | Disable TLS verification |
| `--include-subdomains` | Include subdomains for crawling |
| `--proxy` | Proxy URL (e.g. http://127.0.0.1:8080) |
| `--maxtime` | Max crawl time per URL in seconds |
| `--disable-redirects` | Disable following redirects |
| `--timeout` | HTTP request timeout in seconds (default 30) |
| `--silent` | Suppress banner |
| `--verbose` | Enable verbose logging |
| `-o, --output` | Save results to file |
| `--include-ext` | Only include URLs with these extensions (e.g. `js,php`) |
| `--exclude-ext` | Exclude URLs with these extensions (e.g. `png,css`) |
| `--match` | Regex to include matching URLs |
| `--filter` | Regex to exclude matching URLs |
| `--delay` | Delay between requests in milliseconds |
| `--header` | Custom headers (repeatable) |
| `--cookie` | Cookie string |
| `--emails` | Extract email addresses |
| `--comments` | Extract HTML comments |
| `-j, --json` | JSON lines output |
| `--status-code` | Filter by status codes |
| `--retry` | Retry attempts on failure |
| `--no-robots` | Ignore robots.txt |
| `--version` | Print version |

### Examples

```bash
# Basic crawl
echo "example.com" | urlferret

# Crawl with depth 2, include subdomains
cat urls.txt | urlferret --depth 2 --include-subdomains

# Extract emails and comments
echo "example.com" | urlferret --emails --comments

# Filter by extension, output to file
cat urls.txt | urlferret --include-ext js --output js-files.txt

# JSON output with status codes
echo "example.com" | urlferret --json

# Polite crawl with delay
echo "example.com" | urlferret --delay 500 --max-pages 50

# Proxy + insecure + verbose
echo "example.com" | urlferret --proxy http://127.0.0.1:8080 --insecure --verbose

# Custom headers and cookie
echo "example.com" | urlferret --header "Authorization: Bearer token123" --cookie "session=abc123"
```
