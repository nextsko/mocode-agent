package uidissect

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TokenDrift is one difference between the brand baseline and the current page.
type TokenDrift struct {
	Category   string `json:"category"`
	Value      string `json:"value"`
	Status     string `json:"status"` // off-brand | unused | match
	Suggestion string `json:"suggestion,omitempty"`
}

// Brand is the design-token baseline captured from a reference page.
type Brand struct {
	Name       string    `json:"name"`
	SourceURL  string    `json:"source_url"`
	CapturedAt time.Time `json:"captured_at"`
	Tokens     Tokens    `json:"tokens"`
}

// CompareTokens reports where the current page drifts from the brand tokens.
func CompareTokens(brand, cur Tokens) []TokenDrift {
	out := make([]TokenDrift, 0, len(brand.FontSizes)+len(cur.FontSizes)+len(brand.Spacing)+len(cur.Spacing)+len(brand.Colors)+len(cur.Colors)+len(brand.Radii)+len(cur.Radii))
	out = append(out, driftFloats("font-size", brand.FontSizes, cur.FontSizes, 0.5)...)
	out = append(out, driftFloats("spacing", brand.Spacing, cur.Spacing, 0.5)...)
	out = append(out, driftStrings("color", brand.Colors, cur.Colors)...)
	out = append(out, driftStrings("radius", brand.Radii, cur.Radii)...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Status != out[j].Status {
			return out[i].Status < out[j].Status
		}
		return out[i].Category < out[j].Category
	})
	return out
}

func driftFloats(cat string, brand, cur []float64, tol float64) []TokenDrift {
	var out []TokenDrift
	inBrand := func(v float64) bool {
		for _, b := range brand {
			if abs(b-v) <= tol {
				return true
			}
		}
		return false
	}
	for _, v := range cur {
		if !inBrand(v) {
			out = append(out, TokenDrift{
				Category:   cat,
				Value:      trimFloat(v),
				Status:     "off-brand",
				Suggestion: nearestFloatSuggestion(v, brand),
			})
		}
	}
	for _, b := range brand {
		used := false
		for _, v := range cur {
			if abs(b-v) <= tol {
				used = true
				break
			}
		}
		if !used {
			out = append(out, TokenDrift{Category: cat, Value: trimFloat(b), Status: "unused", Suggestion: "brand value not used on this page"})
		}
	}
	return out
}

func driftStrings(cat string, brand, cur []string) []TokenDrift {
	set := func(in []string) map[string]bool {
		m := make(map[string]bool, len(in))
		for _, v := range in {
			m[strings.ToLower(v)] = true
		}
		return m
	}
	b, c := set(brand), set(cur)
	var out []TokenDrift
	for v := range c {
		if !b[v] {
			out = append(out, TokenDrift{Category: cat, Value: v, Status: "off-brand", Suggestion: nearestStringSuggestion(brand)})
		}
	}
	for v := range b {
		if !c[v] {
			out = append(out, TokenDrift{Category: cat, Value: v, Status: "unused", Suggestion: "brand value not used on this page"})
		}
	}
	return out
}

func nearestFloatSuggestion(v float64, brand []float64) string {
	best := -1.0
	for _, b := range brand {
		if best < 0 || abs(b-v) < abs(best-v) {
			best = b
		}
	}
	if best < 0 {
		return "brand has no value in this category yet"
	}
	return fmt.Sprintf("closest brand value: %spx", trimFloat(best))
}

func nearestStringSuggestion(brand []string) string {
	if len(brand) == 0 {
		return "brand has no value in this category yet"
	}
	return "closest brand value: " + brand[0]
}

// SaveBrand writes the baseline tokens.
func SaveBrand(b Brand, workingDir string) (string, error) {
	dir := Dir(workingDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create ui dir: %w", err)
	}
	raw, err := jsonMarshalIndent(b)
	if err != nil {
		return "", fmt.Errorf("marshal brand: %w", err)
	}
	path := filepath.Join(dir, "brand.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", fmt.Errorf("write brand: %w", err)
	}
	return path, nil
}

// LoadBrand reads the baseline, if captured.
func LoadBrand(workingDir string) (Brand, bool) {
	raw, err := os.ReadFile(filepath.Join(Dir(workingDir), "brand.json"))
	if err != nil {
		return Brand{}, false
	}
	var b Brand
	if err := jsonUnmarshal(raw, &b); err != nil {
		return Brand{}, false
	}
	return b, true
}

// RenderTokenDrift renders the comparison.
func RenderTokenDrift(b Brand, cur Tokens, drifts []TokenDrift) string {
	var sb strings.Builder
	sb.WriteString("# 品牌令牌对比\n\n")
	fmt.Fprintf(&sb, "- Brand: %s (%s)\n- Current font sizes: %d · colors: %d\n\n", firstNonEmpty(b.Name, b.SourceURL), b.SourceURL, len(cur.FontSizes), len(cur.Colors))
	off, unused := 0, 0
	for _, d := range drifts {
		switch d.Status {
		case "off-brand":
			off++
		case "unused":
			unused++
		}
	}
	fmt.Fprintf(&sb, "- off-brand: %d\n- brand values unused on this page: %d\n\n", off, unused)
	if len(drifts) == 0 {
		sb.WriteString("No drift — this page matches the brand tokens.\n")
		return sb.String()
	}
	sb.WriteString("| Status | Category | Value | Suggestion |\n|---|---|---|---|\n")
	for _, d := range drifts {
		fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", d.Status, d.Category, d.Value, d.Suggestion)
	}
	return sb.String()
}
