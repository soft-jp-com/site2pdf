# site2pdf

English | [日本語](README.ja.md)

A command-line tool written in Go that drives Google Chrome to save web pages as **PDF**, **plain text** or **structured JSON**.

## Features

- **Output formats**: PDF with the page layout and styles kept, plain text, structured JSON (title, published date, meta tags, main content without navigation and ads, full text), or all three at once
- **Two ways to run Chrome**
  - **Headless**: finds Chrome on your PC and runs it in the background
  - **Connect to your Chrome**: attaches to a Chrome started with `--remote-debugging-port`, so pages that need a login are saved with your current session
- **Dynamic sites**: renders pages built with JavaScript (React, Vue, Next.js and so on) and expands inner scroll areas
- **Ad blocking**: stops requests to ad networks and hides ad slots (on by default; turn off with `-block-ads=false`)
- **Batch and crawl**: save a single URL, a list of URLs in `sites.json`, or every page under the same domain

## Requirements

- Google Chrome (or Chromium)
- Go 1.22 or later (only to build)

Windows is the main target. The code also builds on macOS and Linux.

## Build

```bash
go build -o dist/site2pdf ./cmd/site2pdf
```

On Windows you can run `build.bat`, which writes `dist\site2pdf.exe`.

## Usage

```bash
# Save as PDF
site2pdf -url "https://example.com" -output "./output/example.pdf"

# Print structured JSON to standard output
site2pdf -url "https://example.com" -json

# Save PDF, text and JSON into a folder
site2pdf -url "https://example.com" -format all -output "./output"

# Use the session of a Chrome you started with --remote-debugging-port=9222
site2pdf -remote-port 9222 -url "https://example.com/members-only" -json

# Save every document listed in sites.json
site2pdf -sites "sites.json"

# Crawl the same domain, up to 20 pages
site2pdf -crawl -url "https://example.com" -output "./output/crawled" -max-pages 20
```

Run `site2pdf -h` for all options. Detailed documents (in Japanese):

- [docs/USAGE.md](docs/USAGE.md): options, the settings file and examples
- [docs/SPEC.md](docs/SPEC.md): architecture, data model and the JSON schema

## Test

```bash
go test ./...
```

## License

[Apache License 2.0](LICENSE)
