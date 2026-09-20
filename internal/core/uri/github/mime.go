package github

import "strings"

// imageExtensions maps the extensions worth returning as images. A raw fetch of
// a PNG is the only way to look at a screenshot committed to a repository, and
// treating it as text would fail the UTF-8 check with a confusing message.
var imageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
}

// imageMIME reports the MIME type for a path with a known image extension.
func imageMIME(name string) (string, bool) {
	lower := strings.ToLower(name)
	for ext, mime := range imageExtensions {
		if strings.HasSuffix(lower, ext) {
			return mime, true
		}
	}
	return "", false
}
