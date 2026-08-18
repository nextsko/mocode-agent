package crawler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/JohannesKaufmann/html-to-markdown/plugin"
	"github.com/PuerkitoBio/goquery"
)

// RecursiveCrawler crawls multiple pages starting from a URL.
type RecursiveCrawler struct {
	opts      *CrawlOptions
	client    *http.Client
	converter *md.Converter
	mu        sync.Mutex
	pages     []CrawlPage
	visited   map[string]bool
	baseURL   *url.URL
}

// NewRecursiveCrawler creates a new RecursiveCrawler.
func NewRecursiveCrawler(opts *CrawlOptions) *RecursiveCrawler {
	if opts == nil {
		opts = DefaultCrawlOptions()
	}

	converter := md.NewConverter("", true, &md.Options{
		HeadingStyle:     "atx",
		HorizontalRule:   "---",
		BulletListMarker: "-",
		CodeBlockStyle:   "fenced",
	})
	converter.Use(plugin.GitHubFlavored())

	return &RecursiveCrawler{
		opts: opts,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		converter: converter,
		visited:   make(map[string]bool),
	}
}

func (rc *RecursiveCrawler) SetClient(client *http.Client) {
	if client != nil {
		rc.client = client
	}
}

// Crawl starts crawling from the given URL.
func (rc *RecursiveCrawler) Crawl(startURL string) (*CrawlResult, error) {
	u, err := url.Parse(startURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	rc.baseURL = u

	// BFS crawl with depth tracking
	type crawlTask struct {
		url   string
		depth int
	}

	queue := []crawlTask{{url: startURL, depth: 0}}

	for len(queue) > 0 {
		rc.mu.Lock()
		if rc.pageCount() >= rc.opts.MaxPages {
			rc.mu.Unlock()
			break
		}
		rc.mu.Unlock()

		task := queue[0]
		queue = queue[1:]

		if task.depth > rc.opts.MaxDepth {
			continue
		}

		rc.mu.Lock()
		if rc.visited[task.url] {
			rc.mu.Unlock()
			continue
		}
		rc.mu.Unlock()

		links, err := rc.crawlPage(task.url)
		if err != nil {
			continue // skip failed pages
		}

		rc.mu.Lock()
		rc.visited[task.url] = true
		rc.mu.Unlock()

		// Extract links for next depth level
		if task.depth < rc.opts.MaxDepth {
			for _, link := range links {
				if rc.shouldFollow(link) {
					queue = append(queue, crawlTask{url: link, depth: task.depth + 1})
				}
			}
		}
	}

	return &CrawlResult{
		Pages: rc.pages,
		Total: len(rc.pages),
	}, nil
}

// crawlPage fetches a single page, converts to Markdown, and returns discovered links.
func (rc *RecursiveCrawler) crawlPage(pageURL string) ([]string, error) {
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := rc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	title := strings.TrimSpace(doc.Find("title").Text())

	// Extract links
	var links []string
	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href, exists := sel.Attr("href")
		if !exists || href == "" {
			return
		}
		absoluteURL := rc.resolveURL(href)
		if absoluteURL != "" {
			links = append(links, absoluteURL)
		}
	})

	// Convert body to markdown
	bodyHTML, err := doc.Find("body").Html()
	if err != nil || bodyHTML == "" {
		return links, nil
	}

	markdown, err := rc.converter.ConvertString(bodyHTML)
	if err != nil || markdown == "" {
		return links, nil
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.pageCount() >= rc.opts.MaxPages {
		return links, nil
	}

	var sb strings.Builder
	if title != "" {
		sb.WriteString(fmt.Sprintf("# %s\n\n", title))
	}
	sb.WriteString(markdown)

	rc.pages = append(rc.pages, CrawlPage{
		URL:      pageURL,
		Title:    title,
		Markdown: sb.String(),
	})

	return links, nil
}

// resolveURL resolves a relative URL to an absolute URL.
func (rc *RecursiveCrawler) resolveURL(href string) string {
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}

	resolved := rc.baseURL.ResolveReference(parsed)

	// Only http/https
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}

	// Remove fragment
	resolved.Fragment = ""

	return resolved.String()
}

// shouldFollow checks if a URL should be followed during crawling.
func (rc *RecursiveCrawler) shouldFollow(linkURL string) bool {
	rc.mu.Lock()
	if rc.visited[linkURL] || rc.pageCount() >= rc.opts.MaxPages {
		rc.mu.Unlock()
		return false
	}
	rc.mu.Unlock()

	parsed, err := url.Parse(linkURL)
	if err != nil {
		return false
	}

	if rc.opts.SameDomain && parsed.Host != rc.baseURL.Host {
		return false
	}

	// Skip non-HTML resources
	path := strings.ToLower(parsed.Path)
	skipExts := []string{".pdf", ".zip", ".tar", ".gz", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".css", ".js", ".woff", ".woff2", ".ttf", ".eot"}
	for _, ext := range skipExts {
		if strings.HasSuffix(path, ext) {
			return false
		}
	}

	return true
}

func (rc *RecursiveCrawler) pageCount() int {
	return len(rc.pages)
}

// QuickCrawl is a convenience function for quick recursive crawling.
func QuickCrawl(startURL string, maxDepth, maxPages int) (*CrawlResult, error) {
	opts := DefaultCrawlOptions()
	if maxDepth > 0 {
		opts.MaxDepth = maxDepth
	}
	if maxPages > 0 {
		opts.MaxPages = maxPages
	}
	return NewRecursiveCrawler(opts).Crawl(startURL)
}
