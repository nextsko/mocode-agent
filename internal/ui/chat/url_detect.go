package chat

import (
	"regexp"
	"strings"
)

// URLDetector 检测文本中的 URL.
type URLDetector struct {
	patterns []*regexp.Regexp
}

// NewURLDetector 创建 URL 检测器.
func NewURLDetector() *URLDetector {
	return &URLDetector{
		patterns: []*regexp.Regexp{
			// HTTP/HTTPS URLs
			regexp.MustCompile(`https?://[^\s<>\"')\]]+`),
			// File paths (Unix: /path/to/file, Windows: C:\path\to\file)
			regexp.MustCompile(`(?:/[a-zA-Z0-9._-]+(?:/[a-zA-Z0-9._-]+)*|[A-Z]:\\[^\s<>\"')\]]+)`),
			// Email addresses
			regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`),
			// IP addresses with ports
			regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(?::\d+)?`),
		},
	}
}

// Detect 检测文本中的 URL.
func (d *URLDetector) Detect(content string) []URLInfo {
	var urls []URLInfo
	seen := make(map[string]bool)

	for _, pattern := range d.patterns {
		matches := pattern.FindAllStringIndex(content, -1)
		for _, match := range matches {
			url := content[match[0]:match[1]]

			// 去重
			if seen[url] {
				continue
			}
			seen[url] = true

			line, col := d.getLineCol(content, match[0])

			urls = append(urls, URLInfo{
				URL:   url,
				Start: match[0],
				End:   match[1],
				Line:  line,
				Col:   col,
			})
		}
	}

	return urls
}

// DetectInLine 检测单行中的 URL.
func (d *URLDetector) DetectInLine(line string, lineNum int) []URLInfo {
	var urls []URLInfo
	seen := make(map[string]bool)

	for _, pattern := range d.patterns {
		matches := pattern.FindAllStringIndex(line, -1)
		for _, match := range matches {
			url := line[match[0]:match[1]]

			// 去重
			if seen[url] {
				continue
			}
			seen[url] = true

			urls = append(urls, URLInfo{
				URL:   url,
				Start: match[0],
				End:   match[1],
				Line:  lineNum,
				Col:   match[0],
			})
		}
	}

	return urls
}

// IsURL 检查字符串是否是 URL.
func (d *URLDetector) IsURL(s string) bool {
	for _, pattern := range d.patterns {
		if pattern.MatchString(s) {
			return true
		}
	}
	return false
}

// GetURLType 获取 URL 类型.
func (d *URLDetector) GetURLType(url string) URLType {
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return URLTypeWeb
	}
	if strings.Contains(url, "@") {
		return URLTypeEmail
	}
	if strings.Contains(url, ":\\") || strings.HasPrefix(url, "/") {
		return URLTypeFile
	}
	if regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`).MatchString(url) {
		return URLTypeIP
	}
	return URLTypeUnknown
}

// getLineCol 计算位置的行号和列号.
func (d *URLDetector) getLineCol(content string, pos int) (int, int) {
	line := 0
	col := 0

	for i, ch := range content {
		if i >= pos {
			break
		}
		if ch == '\n' {
			line++
			col = 0
		} else {
			col++
		}
	}

	return line, col
}

// URLType URL 类型.
type URLType int

const (
	URLTypeUnknown URLType = iota
	URLTypeWeb
	URLTypeEmail
	URLTypeFile
	URLTypeIP
)

// String 返回 URL 类型的字符串表示.
func (t URLType) String() string {
	switch t {
	case URLTypeWeb:
		return "web"
	case URLTypeEmail:
		return "email"
	case URLTypeFile:
		return "file"
	case URLTypeIP:
		return "ip"
	default:
		return "unknown"
	}
}

// URLDetectorWithCustomPatterns 创建带有自定义模式的 URL 检测器.
func NewURLDetectorWithCustomPatterns(patterns []string) (*URLDetector, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, re)
	}

	return &URLDetector{patterns: compiled}, nil
}

// AddPattern 添加自定义模式.
func (d *URLDetector) AddPattern(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	d.patterns = append(d.patterns, re)
	return nil
}

// Patterns 返回所有模式.
func (d *URLDetector) Patterns() []string {
	patterns := make([]string, len(d.patterns))
	for i, p := range d.patterns {
		patterns[i] = p.String()
	}
	return patterns
}
