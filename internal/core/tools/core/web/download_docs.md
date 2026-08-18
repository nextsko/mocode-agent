Download documentation files (.md, .txt) from a GitHub repository by cloning it to memory. Returns the content of all matching files.

<when_to_use>
Use this tool when you need to:
- Download documentation from a GitHub repository
- Get README or docs folder content from a repo
- Quickly gather documentation for analysis or reference

DO NOT use this tool when you need to:
- Crawl a regular website (use crawl instead)
- Fetch a single web page (use fetch instead)
- Clone a full repository (use bash with git clone instead)
</when_to_use>

<usage>
- Provide the GitHub repository URL
- Optionally specify a docs_path to filter to a specific directory
- Optionally set max_files to limit the number of files returned
</usage>

<features>
- Clones repository to memory (no disk I/O)
- Filters by file extension (.md, .txt by default)
- Supports path prefix filtering
- Returns structured Markdown output
</features>

<limitations>
- Only supports GitHub repositories
- Limited to 50 files by default (max 100)
- Each file content truncated to 2000 characters in output
- Requires public repository access
- Does not handle authentication for private repos
</limitations>

<tips>
- Use docs_path to narrow down to specific documentation directories
- For large repos, reduce max_files to avoid overwhelming output
- Works best with repos that have well-organized documentation
</tips>
