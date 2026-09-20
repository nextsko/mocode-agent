package query

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode"
)

// rawToken is a whitespace-delimited token produced by tokenize.
type rawToken struct {
	text string
	// quoted marks a token that was entirely a quoted phrase.
	quoted bool
	// quotedValue marks a directive value that was quoted inline
	// (intitle:"a b").
	quotedValue bool
}

// groupCounter is a process-wide counter issuing OR-group ids that stay
// unique across parsed queries.
var groupCounter atomic.Uint64

// nextGroupID allocates the next unique OR-group id; ids start at 1 so
// that 0 can mark "no group".
func nextGroupID() int {
	return int(groupCounter.Add(1))
}

// isQuote reports whether r is an ASCII or curly double quote.
func isQuote(r rune) bool {
	return r == '"' || r == '\u201c' || r == '\u201d'
}

// tokenize splits a raw query into whitespace-delimited tokens, honoring
// Unicode whitespace (NBSP and friends), quoted spans, and standalone
// parens. It is a hand-written character scanner mirroring the reference
// implementation; regular expressions are reserved for directive and
// date pattern recognition.
func tokenize(raw string) []rawToken {
	runes := []rune(raw)
	n := len(runes)
	var tokens []rawToken
	i := 0
	for i < n {
		ch := runes[i]
		if unicode.IsSpace(ch) {
			i++
			continue
		}
		if isQuote(ch) {
			j := i + 1
			var buf []rune
			for j < n && !isQuote(runes[j]) {
				buf = append(buf, runes[j])
				j++
			}
			if trimmed := strings.TrimSpace(string(buf)); trimmed != "" {
				tokens = append(tokens, rawToken{text: trimmed, quoted: true})
			}
			i = j + 1
			continue
		}
		// Bare word; a quote directly after name: swallows the quoted
		// span into the same token (intitle:"budget tips").
		var buf []rune
		quotedValue := false
		for i < n && !unicode.IsSpace(runes[i]) {
			c := runes[i]
			if isQuote(c) && len(buf) > 0 && buf[len(buf)-1] == ':' {
				j := i + 1
				for j < n && !isQuote(runes[j]) {
					buf = append(buf, runes[j])
					j++
				}
				quotedValue = true
				i = j + 1
				continue
			}
			if isQuote(c) {
				break // foo"bar: stop the word, quote starts a phrase
			}
			buf = append(buf, c)
			i++
		}
		if len(buf) > 0 {
			tokens = append(tokens, rawToken{text: string(buf), quotedValue: quotedValue})
		}
	}
	return splitParens(tokens)
}

// splitParens splits leading ( and unbalanced trailing ) into standalone
// tokens so (react OR vue) parses while site:wikipedia.org/Foo_(bar)
// stays whole.
func splitParens(tokens []rawToken) []rawToken {
	out := make([]rawToken, 0, len(tokens))
	for _, tok := range tokens {
		if tok.quoted || tok.quotedValue {
			out = append(out, tok)
			continue
		}
		text := tok.text
		for strings.HasPrefix(text, "(") {
			out = append(out, rawToken{text: "("})
			text = text[1:]
		}
		trailing := 0
		for strings.HasSuffix(text, ")") {
			// Only strip parens that do not close an opener inside the
			// word.
			body := text[:len(text)-1]
			depth := 0
			for _, c := range body {
				if c == '(' {
					depth++
				} else if c == ')' {
					depth--
				}
			}
			if depth > 0 {
				break
			}
			text = body
			trailing++
		}
		if text != "" {
			out = append(out, rawToken{text: text})
		}
		for k := 0; k < trailing; k++ {
			out = append(out, rawToken{text: ")"})
		}
	}
	return out
}

// isReservedToken reports whether text is an operator, a paren, or a
// recognized directive — anything a bare name: must not adopt as its
// space-separated value.
func isReservedToken(text string) bool {
	switch text {
	case "(", ")", "OR", "AND", "NOT", "|", "||", "&&", "!":
		return true
	}
	m := directivePattern.FindStringSubmatch(text)
	if m == nil {
		return false
	}
	name := strings.ToLower(m[2])
	_, isField := DIRECTIVE_FIELDS[name]
	_, isMode := ALL_MODES[name]
	return isField || isMode
}

// isoDate converts date components to a validated ISO date, or "" when
// any component is out of range.
func isoDate(year, month, day int) string {
	if year < 1000 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31 {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

// ParseDateValue parses a before:/after: value into ISO YYYY-MM-DD.
//
// It accepts YYYY, YYYY-MM, and YYYY-MM-DD (also / and . separators) plus
// MM/DD/YYYY (day-first assumed when the first field exceeds 12). Bare
// years and months resolve to the first day of the period, matching
// Google's after:2024 == after:2024-01-01 semantics. Returns "" when the
// value is not a recognized date.
func ParseDateValue(value string) string {
	t := strings.TrimSpace(value)
	if m := yearFirstDatePattern.FindStringSubmatch(t); m != nil {
		year, _ := strconv.Atoi(m[1])
		month, day := 1, 1
		if m[2] != "" {
			month, _ = strconv.Atoi(m[2])
		}
		if m[3] != "" {
			day, _ = strconv.Atoi(m[3])
		}
		return isoDate(year, month, day)
	}
	if m := yearLastDatePattern.FindStringSubmatch(t); m != nil {
		month, _ := strconv.Atoi(m[1])
		day, _ := strconv.Atoi(m[2])
		if month > 12 && day <= 12 {
			month, day = day, month
		}
		year, _ := strconv.Atoi(m[3])
		return isoDate(year, month, day)
	}
	return ""
}

// ParseSearchQuery parses a raw query into a StructuredQuery.
//
// Lenient by construction: unknown name:value tokens (URLs, C:\paths,
// TS2345:, jargon) stay in the free text verbatim, and a directive with
// an unparseable value (before:someday) degrades to a plain term instead
// of being dropped.
func ParseSearchQuery(raw string) StructuredQuery {
	q := StructuredQuery{Raw: raw}

	tokens := tokenize(raw)
	negateNext := false
	orPending := false
	lastWasTerm := false
	var allMode directiveField

	pushConstraint := func(field directiveField, value string, negated bool) {
		q.HasDirectives = true
		orPending = false
		lastWasTerm = false
		v := strings.TrimSpace(value)
		if v == "" {
			return
		}
		switch field {
		case fieldSite:
			if site := normalizeSite(v); site != "" {
				if negated {
					q.ExcludedSites = append(q.ExcludedSites, site)
				} else {
					q.Sites = append(q.Sites, site)
				}
			}
		case fieldInURL:
			if negated {
				q.ExcludedInURL = append(q.ExcludedInURL, v)
			} else {
				q.InURL = append(q.InURL, v)
			}
		case fieldInTitle:
			if negated {
				q.ExcludedInTitle = append(q.ExcludedInTitle, v)
			} else {
				q.InTitle = append(q.InTitle, v)
			}
		case fieldInText:
			if negated {
				q.ExcludedInText = append(q.ExcludedInText, v)
			} else {
				q.InText = append(q.InText, v)
			}
		case fieldFiletype:
			if ext := strings.TrimLeft(strings.ToLower(v), "."); ext != "" {
				if negated {
					q.ExcludedFiletypes = append(q.ExcludedFiletypes, ext)
				} else {
					q.Filetypes = append(q.Filetypes, ext)
				}
			}
		}
	}

	pushTerm := func(text string, phrase bool) {
		negated := negateNext
		negateNext = false
		if allMode != "" {
			pushConstraint(allMode, text, negated)
			return
		}
		term := QueryTerm{Text: text}
		if phrase {
			term.Phrase = true
		}
		if negated {
			term.Negated = true
		}
		if orPending && lastWasTerm && len(q.Terms) > 0 {
			prev := &q.Terms[len(q.Terms)-1]
			if prev.GroupID == 0 {
				prev.GroupID = nextGroupID()
			}
			term.GroupID = prev.GroupID
		}
		orPending = false
		lastWasTerm = true
		q.Terms = append(q.Terms, term)
	}

	for idx := 0; idx < len(tokens); idx++ {
		tok := tokens[idx]

		if tok.quoted {
			pushTerm(tok.text, true)
			continue
		}

		// Boolean operators and grouping parens.
		switch tok.text {
		case "(", ")":
			continue
		case "OR", "|", "||":
			orPending = true
			q.HasDirectives = true
			continue
		case "AND", "&&":
			q.HasDirectives = true
			continue
		case "NOT", "!":
			negateNext = true
			q.HasDirectives = true
			continue
		case "-", "+":
			// The tokenizer splits -"exact phrase" into - plus a quoted
			// phrase; carry the negation over.
			if tok.text == "-" && idx+1 < len(tokens) && tokens[idx+1].quoted {
				negateNext = true
			}
			continue
		}

		match := directivePattern.FindStringSubmatch(tok.text)
		name := ""
		if match != nil {
			name = strings.ToLower(match[2])
		}
		allMatch := ALL_MODES[name]
		field := DIRECTIVE_FIELDS[name]

		if match != nil && allMatch != "" {
			allMode = allMatch
			q.HasDirectives = true
			// allintitle:budget tips — inline value plus every following
			// term.
			if inline := strings.TrimSpace(match[3]); inline != "" {
				pushConstraint(allMatch, inline, match[1] == "-")
			}
			orPending = false
			lastWasTerm = false
			continue
		}

		if match != nil && field != "" {
			value := strings.TrimSpace(match[3])
			// site: example.com — lenient: adopt the next plain token as
			// the value.
			if value == "" && idx+1 < len(tokens) {
				next := tokens[idx+1]
				if next.quoted || !isReservedToken(next.text) {
					value = strings.TrimSpace(next.text)
					idx++
				}
			}
			if value == "" {
				q.HasDirectives = true
				continue
			}
			negated := match[1] == "-" || negateNext
			negateNext = false
			switch field {
			case fieldBefore, fieldAfter:
				iso := ParseDateValue(value)
				if iso == "" {
					pushTerm(tok.text, false)
					continue
				}
				bound := iso
				if field == fieldBefore {
					q.Before = &bound
				} else {
					q.After = &bound
				}
				q.HasDirectives = true
				orPending = false
				lastWasTerm = false
			case fieldLang:
				lang := strings.ToLower(value)
				q.Lang = &lang
				q.HasDirectives = true
				orPending = false
				lastWasTerm = false
			default:
				pushConstraint(field, value, negated)
			}
			continue
		}

		// Plain term with optional +/- prefix.
		text := tok.text
		if strings.HasPrefix(text, "-") && len(text) > 1 {
			negateNext = true
			q.HasDirectives = true
			text = strings.TrimLeft(text, "-")
			if text == "" {
				continue
			}
			// A directive revealed after dash stripping (e.g. --site:x
			// or a pre-split - site:x); re-check it.
			negMatch := directivePattern.FindStringSubmatch(text)
			var negField directiveField
			if negMatch != nil {
				negField = DIRECTIVE_FIELDS[strings.ToLower(negMatch[2])]
			}
			if negMatch != nil && negField != "" &&
				negField != fieldBefore && negField != fieldAfter && negField != fieldLang {
				negateNext = false
				pushConstraint(negField, strings.TrimSpace(negMatch[3]), true)
				continue
			}
			pushTerm(text, false)
			continue
		}
		if strings.HasPrefix(text, "+") && len(text) > 1 {
			// Legacy Google +term: verbatim/required — treat as an exact
			// phrase.
			pushTerm(text[1:], true)
			q.HasDirectives = true
			continue
		}
		pushTerm(text, false)
	}

	q.Text = renderTerms(q.Terms, QuerySyntax{Phrases: true, Negation: true, Or: true})
	q.HasConstraints = len(q.Sites) > 0 ||
		len(q.ExcludedSites) > 0 ||
		len(q.InURL) > 0 ||
		len(q.ExcludedInURL) > 0 ||
		len(q.InTitle) > 0 ||
		len(q.ExcludedInTitle) > 0 ||
		len(q.Filetypes) > 0 ||
		len(q.ExcludedFiletypes) > 0 ||
		q.Before != nil ||
		q.After != nil
	return q
}
