package uidissect

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// FromHTML builds a static map input from server-rendered HTML. It is the
// no-browser fallback: structure, text and semantics are available, but boxes
// and computed styles are zero, so layout and contrast rules do not fire.
func FromHTML(html, url string) (Raw, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return Raw{}, fmt.Errorf("parse html: %w", err)
	}
	raw := Raw{URL: url, Title: strings.TrimSpace(doc.Find("title").First().Text())}
	if snippet := strings.TrimSpace(doc.Find(`meta[name="viewport"]`).AttrOr("content", "")); snippet == "" {
		raw.Viewport.W, raw.Viewport.H = 0, 0
	}

	var walk func(s *goquery.Selection, depth int, parentPath string)
	walk = func(s *goquery.Selection, depth int, parentPath string) {
		if len(raw.Nodes) >= 1200 || depth > 14 {
			return
		}
		s.Each(func(_ int, el *goquery.Selection) {
			if len(raw.Nodes) >= 1200 {
				return
			}
			tag := goquery.NodeName(el)
			if skipTag(tag) {
				return
			}
			path := tag
			if id, ok := el.Attr("id"); ok && id != "" {
				path = "#" + id
			}
			if parentPath != "" {
				path = parentPath + " > " + path
			}
			classes := strings.Fields(el.AttrOr("class", ""))
			text := ""
			if el.Children().Length() == 0 {
				text = strings.TrimSpace(el.Text())
				if len(text) > 140 {
					text = text[:140]
				}
			}
			raw.Nodes = append(raw.Nodes, Node{
				Path:        path,
				Tag:         tag,
				ID:          el.AttrOr("id", ""),
				Classes:     classes,
				Role:        el.AttrOr("role", ""),
				AriaLabel:   el.AttrOr("aria-label", ""),
				Text:        text,
				Depth:       depth,
				ChildCount:  el.Children().Length(),
				Interactive: interactiveTag(tag) || el.AttrOr("role", "") == "button",
				ImgAlt:      el.AttrOr("alt", ""),
				Href:        el.AttrOr("href", ""),
				Value:       el.AttrOr("value", ""),
			})
			walk(el.Children(), depth+1, path)
		})
	}
	body := doc.Find("body").First()
	if body.Length() == 0 {
		body = doc.Selection
	}
	walk(body, 0, "")
	return raw, nil
}

func skipTag(tag string) bool {
	switch tag {
	case "script", "style", "noscript", "template", "head", "meta", "link", "br", "hr":
		return true
	}
	return false
}

func interactiveTag(tag string) bool {
	switch tag {
	case "a", "button", "input", "select", "textarea", "summary", "details", "label":
		return true
	}
	return false
}
