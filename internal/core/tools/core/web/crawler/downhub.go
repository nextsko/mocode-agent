package crawler

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"
)

var errMaxFilesReached = errors.New("max files reached")

// Downhub downloads documentation files from a GitHub repository.
type Downhub struct {
	opts *DownhubOptions
}

// NewDownhub creates a new Downhub.
func NewDownhub() *Downhub {
	return &Downhub{opts: DefaultDownhubOptions()}
}

// URL sets the repository URL.
func (d *Downhub) URL(url string) *Downhub {
	d.opts.RepoURL = url
	return d
}

// Path sets the documentation path prefix.
func (d *Downhub) Path(path string) *Downhub {
	d.opts.DocsPath = path
	return d
}

// Extensions sets the file extension filter.
func (d *Downhub) Extensions(exts ...string) *Downhub {
	d.opts.Extensions = exts
	return d
}

// MaxFiles sets the maximum number of files to download.
func (d *Downhub) MaxFiles(n int) *Downhub {
	d.opts.MaxFiles = n
	return d
}

func (d *Downhub) Proxy(url string) *Downhub {
	d.opts.ProxyURL = url
	return d
}

// Fetch downloads documentation files from the repository.
func (d *Downhub) Fetch() (*DocsResult, error) {
	if d.opts.RepoURL == "" {
		return nil, fmt.Errorf("repository URL is required")
	}

	result := &DocsResult{
		RepoURL: d.opts.RepoURL,
		Files:   []DocFile{},
	}

	// Parse owner and repo from URL
	if strings.Contains(d.opts.RepoURL, "github.com/") {
		parts := strings.Split(d.opts.RepoURL, "github.com/")
		if len(parts) > 1 {
			repoParts := strings.Split(strings.TrimSuffix(parts[1], ".git"), "/")
			if len(repoParts) >= 2 {
				result.Owner = repoParts[0]
				result.Repo = repoParts[1]
			}
		}
	}

	// Clone repository to memory
	cloneOptions := &git.CloneOptions{
		URL:   d.opts.RepoURL,
		Depth: 1,
	}
	if d.opts.ProxyURL != "" {
		cloneOptions.ProxyOptions = transport.ProxyOptions{URL: d.opts.ProxyURL}
	}
	r, err := git.Clone(memory.NewStorage(), nil, cloneOptions)
	if err != nil {
		return nil, fmt.Errorf("clone failed: %w", err)
	}

	ref, err := r.Head()
	if err != nil {
		return nil, fmt.Errorf("get HEAD: %w", err)
	}

	commit, err := r.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("get commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	err = tree.Files().ForEach(func(f *object.File) error {
		if len(result.Files) >= d.opts.MaxFiles {
			return errMaxFilesReached
		}

		if d.shouldInclude(f.Name) {
			content, err := f.Contents()
			if err != nil {
				return nil
			}
			result.Files = append(result.Files, DocFile{
				Path:    f.Name,
				Content: content,
			})
		}
		return nil
	})

	if err != nil && !errors.Is(err, errMaxFilesReached) {
		return nil, fmt.Errorf("walk tree: %w", err)
	}

	result.Count = len(result.Files)
	return result, nil
}

// shouldInclude checks if a file should be included based on path and extension.
func (d *Downhub) shouldInclude(filename string) bool {
	if d.opts.DocsPath != "" {
		if !strings.HasPrefix(filename, d.opts.DocsPath+"/") && filename != d.opts.DocsPath {
			return false
		}
	}

	ext := strings.ToLower(filepath.Ext(filename))
	return slices.Contains(d.opts.Extensions, ext)
}

// ToMarkdown converts the result to a Markdown string.
func (r *DocsResult) ToMarkdown() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s/%s\n\n", r.Owner, r.Repo))
	sb.WriteString(fmt.Sprintf("共 %d 个文档文件\n\n", r.Count))

	for _, f := range r.Files {
		sb.WriteString(fmt.Sprintf("## %s\n\n", f.Path))
		sb.WriteString("```\n")
		content := f.Content
		if utf8.RuneCountInString(content) > 2000 {
			runes := []rune(content)
			content = string(runes[:2000]) + "\n... (内容已截断)"
		}
		sb.WriteString(content)
		sb.WriteString("\n```\n\n")
	}

	return sb.String()
}

// QuickDownload is a convenience function for downloading docs from a repo.
func QuickDownload(repoURL string) (*DocsResult, error) {
	return NewDownhub().URL(repoURL).Fetch()
}

// QuickDownloadPath is a convenience function for downloading docs from a specific path.
func QuickDownloadPath(repoURL, docsPath string) (*DocsResult, error) {
	return NewDownhub().URL(repoURL).Path(docsPath).Fetch()
}
