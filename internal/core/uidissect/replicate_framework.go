package uidissect

import (
	"strings"
)

// Targets a component skeleton can be emitted for.
const (
	TargetHTML   = "html"
	TargetReact  = "react"
	TargetVue    = "vue"
	TargetSvelte = "svelte"
)

// SkeletonFor emits a component skeleton for a framework target.
func SkeletonFor(m Map, comp Component, target string) (code, css string) {
	htmlOut, cssOut := Skeleton(m, comp)
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "", TargetHTML:
		return htmlOut, cssOut
	case TargetReact:
		return toReact(htmlOut), cssOut
	case TargetVue:
		return "<template>\n" + indentBlock(htmlOut, "  ") + "</template>\n\n<script setup>\n// props go here\n</script>\n\n<style scoped>\n" + cssOut + "</style>\n", cssOut
	case TargetSvelte:
		return htmlOut + "\n<style>\n" + cssOut + "</style>\n", cssOut
	default:
		return htmlOut, cssOut
	}
}

// ExtensionFor returns the file extension for a target.
func ExtensionFor(target string) string {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case TargetReact:
		return ".jsx"
	case TargetVue:
		return ".vue"
	case TargetSvelte:
		return ".svelte"
	default:
		return ".html"
	}
}

// toReact converts the HTML skeleton to JSX: class -> className and
// void elements self-closed.
func toReact(html string) string {
	r := strings.NewReplacer(
		"class=", "className=",
		"<img ", "<img ",
		"<input ", "<input ",
		"<br>", "<br />",
		"<hr>", "<hr />",
	)
	out := r.Replace(html)
	out = strings.ReplaceAll(out, "<img ", "<img ")
	// Self-close void elements that the HTML emitter writes without a slash.
	for _, tag := range []string{"img", "input"} {
		out = closeVoid(out, tag)
	}
	return out
}

func closeVoid(s, tag string) string {
	open := "<" + tag + " "
	var b strings.Builder
	for {
		i := strings.Index(s, open)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		j := strings.Index(s[i:], ">")
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		seg := s[i : i+j]
		if strings.HasSuffix(seg, "/") {
			b.WriteString(s[:i+j+1])
		} else {
			b.WriteString(seg + " />")
		}
		s = s[i+j+1:]
	}
}

func indentBlock(s, prefix string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n") + "\n"
}
