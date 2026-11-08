// Package uidissect turns a live page into a structural map: every visible
// box, the components it belongs to, the design tokens it uses, and the
// aesthetic problems worth fixing. It is framework-free; the browser tool
// supplies the raw DOM measurement.
package uidissect

import "time"

// Rect is a box in page coordinates.
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Styles is the subset of computed styles that explains layout and look.
type Styles struct {
	FontSize   float64 `json:"font_size"`
	FontWeight string  `json:"font_weight"`
	LineHeight float64 `json:"line_height"`
	Color      string  `json:"color"`
	Background string  `json:"background"`
	MarginTop  float64 `json:"margin_top"`
	MarginBot  float64 `json:"margin_bottom"`
	MarginLeft float64 `json:"margin_left"`
	MarginRght float64 `json:"margin_right"`
	PadTop     float64 `json:"padding_top"`
	PadBot     float64 `json:"padding_bottom"`
	PadLeft    float64 `json:"padding_left"`
	PadRght    float64 `json:"padding_right"`
	Radius     string  `json:"radius"`
	Shadow     bool    `json:"shadow"`
	Display    string  `json:"display"`
	Position   string  `json:"position"`
	ZIndex     string  `json:"z_index"`
	OverflowX  bool    `json:"overflow_x"`
	OverflowY  bool    `json:"overflow_y"`
}

// Node is one visible element measured in the page.
type Node struct {
	Path        string   `json:"path"`
	Tag         string   `json:"tag"`
	ID          string   `json:"id,omitempty"`
	Classes     []string `json:"classes,omitempty"`
	Role        string   `json:"role,omitempty"`
	AriaLabel   string   `json:"aria_label,omitempty"`
	Text        string   `json:"text,omitempty"`
	Rect        Rect     `json:"rect"`
	Styles      Styles   `json:"styles"`
	Depth       int      `json:"depth"`
	ChildCount  int      `json:"child_count"`
	Interactive bool     `json:"interactive"`
	Disabled    bool     `json:"disabled,omitempty"`
	ImgAlt      string   `json:"img_alt,omitempty"`
	Href        string   `json:"href,omitempty"`
	Value       string   `json:"value,omitempty"`
}

// Raw is what the page collector returns before analysis.
type Raw struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Viewport struct {
		W   float64 `json:"w"`
		H   float64 `json:"h"`
		DPR float64 `json:"dpr"`
	} `json:"viewport"`
	Nodes []Node `json:"nodes"`
}

// Component is a coherent slice of the page: a landmark, a card, a form.
type Component struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Path       string `json:"path"`
	Rect       Rect   `json:"rect"`
	NodeCount  int    `json:"node_count"`
	Repeat     int    `json:"repeat,omitempty"`
	TextSample string `json:"text_sample,omitempty"`
}

// Finding is one aesthetic or structural problem.
type Finding struct {
	Category   string `json:"category"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	Selector   string `json:"selector,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

// Tokens are the reusable design values observed on the page.
type Tokens struct {
	FontSizes []float64 `json:"font_sizes"`
	Spacing   []float64 `json:"spacing"`
	Colors    []string  `json:"colors"`
	Radii     []string  `json:"radii"`
	Shadows   int       `json:"shadows"`
}

// Stats summarise the measured page.
type Stats struct {
	Nodes      int `json:"nodes"`
	Components int `json:"components"`
	Findings   int `json:"findings"`
	Depth      int `json:"depth"`
}

// Map is the complete dissection of one page.
type Map struct {
	URL         string      `json:"url"`
	Title       string      `json:"title"`
	Viewport    Rect        `json:"viewport"`
	GeneratedAt time.Time   `json:"generated_at"`
	Nodes       []Node      `json:"nodes"`
	Components  []Component `json:"components"`
	Findings    []Finding   `json:"findings"`
	Tokens      Tokens      `json:"tokens"`
	Stats       Stats       `json:"stats"`
}
