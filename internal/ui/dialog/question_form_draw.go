package dialog

import (
	"fmt"
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/nextsko/mocode-agent/internal/core/question"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
)

func (f *QuestionForm) Height(width int) int {
	h := 0
	if f.showTabs {
		h = 4 // bordered tab row (top + label + bottom) + blank line
	}
	maxQ := 0
	for _, q := range f.questions {
		if qh := q.Height(width); qh > maxQ {
			maxQ = qh
		}
	}
	if f.confirmComp != nil {
		if ch := f.confirmComp.Height(width); ch > maxQ {
			maxQ = ch
		}
	}
	h += maxQ
	return h
}

// CollapsedHeight returns the height of the collapsed summary
// line shown when the editor area is not focused.

func (f *QuestionForm) CollapsedHeight() int { return 1 }

// DrawCollapsed renders a compact one-line summary of the form
// when the user has tabbed away to the chat. For multi-question
// batches it shows the active question text and answered count;
// for single questions it shows just the question text.

func (f *QuestionForm) DrawCollapsed(scr uv.Screen, area uv.Rectangle) {
	icon := f.Styles.Editor.PromptQuestionIconBlurred.Render()
	iconWidth := lipgloss.Width(icon)
	textStyle := f.Styles.Messages.AssistantInfoModel
	countStyle := f.Styles.Messages.AssistantInfoProvider
	lineStyle := f.Styles.Section.Line

	var plainText string
	var confirmRendered string
	if f.numQuestions > 1 {
		answered := 0
		for i := 0; i < f.numQuestions; i++ {
			if f.isAnswered(i) {
				answered++
			}
		}
		if f.isConfirmTab() && f.confirmComp != nil {
			plainText = f.confirmComp.Title
			confirmRendered = f.Styles.Editor.QuestionUnselected.Render(f.confirmComp.Title)
		} else if f.activeIdx < len(f.questions) {
			plainText = f.getQuestionText(f.activeIdx)
		}
		count := fmt.Sprintf("(%d/%d answered)", answered, f.numQuestions)
		plainLabel := plainText + " " + count
		textWidth := iconWidth + 1 + lipgloss.Width(plainLabel)
		remaining := area.Dx() - textWidth - 1

		var rendered string
		if confirmRendered != "" {
			rendered = fmt.Sprintf("%s%s %s", icon, confirmRendered, countStyle.Render(count))
		} else {
			rendered = fmt.Sprintf("%s%s %s", icon, textStyle.Render(plainText), countStyle.Render(count))
		}
		if remaining > 0 {
			rendered = rendered + " " + lineStyle.Render(strings.Repeat(styles.SectionSeparator, remaining))
		}
		drawStyledText(scr, area, rendered)
	} else if f.numQuestions == 1 {
		plainText = f.getQuestionText(0)
		textWidth := iconWidth + 1 + lipgloss.Width(plainText)
		remaining := area.Dx() - textWidth - 1
		rendered := fmt.Sprintf("%s%s", icon, textStyle.Render(plainText))
		if remaining > 0 {
			rendered = rendered + " " + lineStyle.Render(strings.Repeat(styles.SectionSeparator, remaining))
		}
		drawStyledText(scr, area, rendered)
	}
}

// getQuestionText returns the question text for the given index.

func (f *QuestionForm) getQuestionText(idx int) string {
	type hasRequest interface {
		GetRequest() question.Question
	}
	if idx < len(f.questions) {
		if hr, ok := f.questions[idx].(hasRequest); ok {
			return hr.GetRequest().Text
		}
	}
	if idx < len(f.labels) {
		return f.labels[idx]
	}
	return ""
}

// Draw renders the tab bar and the active tab content. When
// showTabs is false (single question), renders content directly
// without tab chrome.

func (f *QuestionForm) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	contentY := area.Min.Y

	if f.showTabs {
		const tabPadX = 1
		tabHeight := 3

		// Compute display labels.
		labels := make([]string, len(f.labels))
		copy(labels, f.labels)

		// Truncate if tabs exceed width. Distribute the available
		// space fairly: short labels keep their natural width and
		// the deficit is shared proportionally among longer ones,
		// with remainder cells distributed left-to-right so the
		// layout resizes smoothly pixel-by-pixel.
		tabWidths := make([]int, len(labels))
		naturalWidths := make([]int, len(labels))
		totalWidth := 0
		for i, l := range labels {
			w := ansi.StringWidth(l) + tabPadX*2 + 2
			tabWidths[i] = w
			naturalWidths[i] = w
			totalWidth += w
		}
		avail := area.Dx()
		if totalWidth > avail && len(labels) > 0 {
			const minLabelW = 1
			minTabW := minLabelW + tabPadX*2 + 2
			n := len(labels)

			// Check if there's enough room to show all tabs with
			// at least a useful label. If each tab can't fit at
			// least 5 cells of label, switch to single-tab mode
			// with a "N of M" counter.
			usefulMinTabW := 5 + tabPadX*2 + 2
			if avail/n < usefulMinTabW {
				// Single-tab mode: show only the active tab
				// label plus a counter.
				counter := fmt.Sprintf("%d/%d", f.activeIdx+1, n)
				activeLabel := labels[f.activeIdx]
				combined := activeLabel + " · " + counter
				maxLabel := avail - tabPadX*2 - 2
				if maxLabel < 3 {
					maxLabel = 3
				}
				if ansi.StringWidth(combined) > maxLabel {
					// Truncate the label part to fit.
					counterPart := " · " + counter
					labelBudget := maxLabel - ansi.StringWidth(counterPart)
					if labelBudget < 1 {
						labelBudget = 1
					}
					combined = ansi.Truncate(activeLabel, labelBudget, "…") + counterPart
				}
				for i := range labels {
					if i == f.activeIdx {
						labels[i] = combined
					} else {
						labels[i] = ""
					}
				}
				// Recalculate widths for single visible tab.
				totalWidth = 0
				for i := range labels {
					if labels[i] == "" {
						tabWidths[i] = 0
					} else {
						w := ansi.StringWidth(labels[i]) + tabPadX*2 + 2
						tabWidths[i] = w
						totalWidth += w
					}
				}
			} else {
				// Normal truncation: distribute space fairly.
				capped := make([]bool, n)
				for {
					freeCount := 0
					freeTotal := 0
					for i := range n {
						if capped[i] {
							continue
						}
						freeCount++
						freeTotal += naturalWidths[i]
					}
					if freeCount == 0 {
						break
					}
					budget := avail
					for i := range n {
						if capped[i] {
							budget -= tabWidths[i]
						}
					}
					share := budget / freeCount
					changed := false
					for i := range n {
						if !capped[i] && naturalWidths[i] <= share {
							capped[i] = true
							tabWidths[i] = naturalWidths[i]
							changed = true
						}
					}
					if !changed {
						for i := range n {
							if !capped[i] {
								tabWidths[i] = max(share, minTabW)
							}
						}
						remainder := budget - share*freeCount
						for i := range n {
							if remainder <= 0 {
								break
							}
							if !capped[i] && tabWidths[i] < naturalWidths[i] {
								tabWidths[i]++
								remainder--
							}
						}
						break
					}
				}

				// Apply truncation based on final widths.
				for i, l := range labels {
					labelAvail := max(tabWidths[i]-tabPadX*2-2, minLabelW)
					if ansi.StringWidth(l) > labelAvail {
						labels[i] = ansi.Truncate(l, labelAvail, "…")
					}
				}
			}
		}

		// Build tab layers for click hit detection.
		var layers []*lipgloss.Layer
		x := area.Min.X

		// Determine hovered tab via simple bounds check.
		hoveredTab := -1
		if f.hoverY >= area.Min.Y && f.hoverY < area.Min.Y+tabHeight {
			tx := area.Min.X
			for i := range labels {
				tw := tabWidths[i]
				if f.hoverX >= tx && f.hoverX < tx+tw {
					hoveredTab = i
					break
				}
				tx += tw
			}
		}

		firstVisible := -1
		for i := range labels {
			if tabWidths[i] > 0 {
				firstVisible = i
				break
			}
		}

		for i, label := range labels {
			// Skip hidden tabs (single-tab mode).
			if tabWidths[i] == 0 {
				continue
			}
			isActive := i == f.activeIdx
			isHovered := i == hoveredTab && !isActive
			labelWidth := ansi.StringWidth(label)
			tabWidth := tabWidths[i]

			tabArea := image.Rect(x, area.Min.Y, x+tabWidth, area.Min.Y+tabHeight)

			border := f.Styles.Tab.InactiveBorder
			textStyle := f.Styles.Tab.InactiveStyle
			if !f.focused {
				border = f.Styles.Tab.InactiveBorderBlurred
			}
			if isActive {
				border = f.Styles.Tab.ActiveBorder
				textStyle = f.Styles.Tab.ActiveStyle
				if !f.focused {
					border = f.Styles.Tab.ActiveBorderBlurred
				}
			} else if i < f.numQuestions && f.isAnswered(i) {
				textStyle = f.Styles.Tab.ActiveStyle
			}
			if isHovered {
				hovered := textStyle
				hovered.Attrs |= uv.AttrBold
				textStyle = hovered
			}

			if i == firstVisible {
				if isActive {
					border.BottomLeft = uv.Side{Content: "┘", Style: border.BottomLeft.Style}
				} else {
					border.BottomLeft = uv.Side{Content: "┴", Style: border.BottomLeft.Style}
				}
			}

			border.Draw(scr, tabArea)

			innerWidth := tabWidth - 2
			xOff := (innerWidth - labelWidth) / 2
			innerArea := image.Rect(
				tabArea.Min.X+1+xOff, tabArea.Min.Y+1,
				tabArea.Max.X-1, tabArea.Max.Y-1,
			)
			uv.NewStyledString(textStyle.Styled(label)).Draw(scr, innerArea)

			// Create an invisible hit layer for this tab.
			hitStr := strings.Repeat(strings.Repeat(" ", tabWidth)+"\n", tabHeight-1) + strings.Repeat(" ", tabWidth)
			layers = append(layers, lipgloss.NewLayer(hitStr).X(x).Y(area.Min.Y).ID(fmt.Sprintf("tab_%d", i)))

			x += tabWidth
		}

		f.compositor = lipgloss.NewCompositor(layers...)

		lineY := area.Min.Y + tabHeight - 1
		lineSide := f.Styles.Tab.InactiveBorder.Bottom
		if !f.focused {
			lineSide = f.Styles.Tab.InactiveBorderBlurred.Bottom
		}
		for lx := x; lx < area.Max.X; lx++ {
			c := uv.NewCell(scr.WidthMethod(), lineSide.Content)
			if c != nil {
				c.Style = lineSide.Style
			}
			scr.SetCell(lx, lineY, c)
		}

		contentY = area.Min.Y + tabHeight + 1
	} else {
		f.compositor = nil
	}

	contentArea := image.Rect(area.Min.X, contentY, area.Max.X, area.Max.Y)

	if f.isConfirmTab() {
		return f.confirmComp.Draw(scr, contentArea)
	}
	if f.activeIdx < f.numQuestions {
		cur := f.questions[f.activeIdx].Draw(scr, contentArea)
		if cur != nil {
			cur.Y += contentY - area.Min.Y
		}
		return cur
	}
	return nil
}

// HeightChanged reports whether any component's height changed.

func (f *QuestionForm) HeightChanged() bool {
	for _, q := range f.questions {
		if q.HeightChanged() {
			return true
		}
	}
	if f.confirmComp != nil && f.confirmComp.HeightChanged() {
		return true
	}
	return false
}

// SetFocused updates focus state for the active tab.
