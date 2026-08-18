package crawler

// ScrapeResult represents the result of scraping a single page.
type ScrapeResult struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
}

// DocFile represents a downloaded documentation file.
type DocFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// DocsResult represents the result of downloading documentation from a repo.
type DocsResult struct {
	RepoURL string    `json:"repo_url"`
	Owner   string    `json:"owner"`
	Repo    string    `json:"repo"`
	Files   []DocFile `json:"files"`
	Count   int       `json:"count"`
}

// CrawlPage represents a single crawled page.
type CrawlPage struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
}

// CrawlResult represents the result of a recursive crawl.
type CrawlResult struct {
	Pages []CrawlPage `json:"pages"`
	Total int         `json:"total"`
}

// CrawlOptions configures the recursive crawler.
type CrawlOptions struct {
	MaxDepth   int  // Maximum crawl depth
	MaxPages   int  // Maximum number of pages to crawl
	SameDomain bool // Restrict to same domain
}

// DownhubOptions configures the documentation downloader.
type DownhubOptions struct {
	RepoURL    string   // GitHub repository URL
	DocsPath   string   // Documentation path prefix (e.g. "docs")
	Extensions []string // File extension filter (default .md, .txt)
	MaxFiles   int      // Maximum number of files
	ProxyURL   string   // Optional proxy URL
}

// DefaultCrawlOptions returns default crawl options.
func DefaultCrawlOptions() *CrawlOptions {
	return &CrawlOptions{
		MaxDepth:   2,
		MaxPages:   20,
		SameDomain: true,
	}
}

// DefaultDownhubOptions returns default download options.
func DefaultDownhubOptions() *DownhubOptions {
	return &DownhubOptions{
		Extensions: []string{".md", ".txt"},
		MaxFiles:   50,
	}
}
