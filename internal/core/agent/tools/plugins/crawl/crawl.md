Crawl multiple pages from a website recursively and convert them to Markdown. Returns structured content from all crawled pages.

<when_to_use>
Use this tool when you need to:
- Crawl a website to gather content from multiple linked pages
- Extract documentation from a website with multiple pages
- Build a comprehensive understanding of a website's content
- Research a topic across multiple pages of a site

DO NOT use this tool when you need to:
- Fetch a single page only (use fetch instead)
- Download files from a GitHub repo (use download_docs instead)
</when_to_use>

<usage>
- Provide the starting URL to begin crawling
- Optionally set max_depth to control how deep to follow links (default: 2)
- Optionally set max_pages to limit total pages crawled (default: 20)
</usage>

<features>
- Recursive BFS crawling with configurable depth
- Automatic HTML to Markdown conversion (GitHub Flavored)
- Same-domain restriction by default
- Skips non-HTML resources (images, PDFs, etc.)
- Returns structured results with URL, title, and content per page
</features>

<limitations>
- Maximum 50 pages per crawl
- Maximum depth of 5
- Each page limited to 5MB
- Only supports HTTP/HTTPS
- May be blocked by some websites
- Does not handle authentication
</limitations>

<tips>
- Start with shallow depth (1-2) to avoid crawling too many pages
- Use max_pages to control resource usage
- For single pages, use the fetch tool instead
</tips>
