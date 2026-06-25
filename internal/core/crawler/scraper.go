package crawler

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/JohannesKaufmann/html-to-markdown/plugin"
	"github.com/PuerkitoBio/goquery"
)

// Scraper fetches web pages and converts them to Markdown.
type Scraper struct {
	client    *http.Client
	converter *md.Converter
}

// NewScraper creates a new Scraper with default settings.
func NewScraper() *Scraper {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	converter := md.NewConverter("", true, &md.Options{
		HeadingStyle:     "atx",
		HorizontalRule:   "---",
		BulletListMarker: "-",
		CodeBlockStyle:   "fenced",
	})
	converter.Use(plugin.GitHubFlavored())

	return &Scraper{
		client:    client,
		converter: converter,
	}
}

// FetchToMarkdown fetches a URL and converts the HTML to Markdown.
func (s *Scraper) FetchToMarkdown(url string) (*ScrapeResult, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Limit to 5MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	title := strings.TrimSpace(doc.Find("title").Text())

	bodyHTML, err := doc.Find("body").Html()
	if err != nil {
		return nil, fmt.Errorf("extract body: %w", err)
	}
	if bodyHTML == "" {
		return nil, fmt.Errorf("extract body: empty body")
	}

	markdown, err := s.converter.ConvertString(bodyHTML)
	if err != nil {
		return nil, fmt.Errorf("convert to markdown: %w", err)
	}

	result := &ScrapeResult{
		URL:   url,
		Title: title,
	}

	var sb strings.Builder
	if title != "" {
		sb.WriteString(fmt.Sprintf("# %s\n\n", title))
	}
	sb.WriteString(markdown)
	result.Markdown = sb.String()

	return result, nil
}

// QuickFetch is a convenience function for quick single-page scraping.
func QuickFetch(url string) (*ScrapeResult, error) {
	return NewScraper().FetchToMarkdown(url)
}
