package model

import (
	"image"
	"slices"

	"github.com/charmbracelet/ultraviolet/layout"
)

func (m *UI) updateLayoutAndSize() {
	// Determine if we should be in compact mode
	if m.state == uiChat {
		if m.forceCompactMode {
			m.isCompact = true
		} else if m.width < compactModeWidthBreakpoint || m.height < compactModeHeightBreakpoint {
			m.isCompact = true
		} else {
			m.isCompact = false
		}
	}

	// First pass sizes components from the current textarea height.
	m.layout = m.generateLayout(m.width, m.height)
	prevHeight := m.textarea.Height()
	m.updateSize()

	// SetWidth can change textarea height due to soft-wrap recalculation.
	// If that happens, run one reconciliation pass with the new height.
	if m.textarea.Height() != prevHeight {
		m.layout = m.generateLayout(m.width, m.height)
		m.updateSize()
	}
}

func (m *UI) updateSize() {
	m.chat.SetSize(m.layout.main.Dx(), m.layout.main.Dy())
	m.textarea.MaxHeight = TextareaMaxHeight
	m.textarea.SetWidth(m.layout.editor.Dx())
	m.renderPills()
}

func (m *UI) generateLayout(w, h int) uiLayout {
	// The screen area we're working with
	area := image.Rect(0, 0, w, h)

	// The help height
	helpHeight := 1
	// The editor height: textarea height + margin for attachments and bottom spacing.
	editorHeight := m.textarea.Height() + editorHeightMargin
	// The sidebar width
	sidebarWidth := 30
	// The header height
	const headerHeight = 1

	// Add app margins
	var appRect, helpRect image.Rectangle
	layout.Vertical(
		layout.Len(area.Dy()-helpHeight),
		layout.Fill(1),
	).Split(area).Assign(&appRect, &helpRect)
	appRect.Min.Y += 1
	appRect.Max.Y -= 1
	helpRect.Min.Y -= 1
	appRect.Min.X += 1
	appRect.Max.X -= 1

	if slices.Contains([]uiState{uiOnboarding, uiInitialize, uiLanding}, m.state) {
		// extra padding on left and right for these states
		appRect.Min.X += 1
		appRect.Max.X -= 1
	}

	uiLayout := uiLayout{
		area:   area,
		status: helpRect,
	}

	// Handle different app states
	switch m.state {
	case uiOnboarding, uiInitialize:
		// Layout
		//
		// header
		// ------
		// main
		// ------
		// help

		var headerRect, mainRect image.Rectangle
		layout.Vertical(
			layout.Len(headerHeight),
			layout.Fill(1),
		).Split(appRect).Assign(&headerRect, &mainRect)
		uiLayout.header = headerRect
		uiLayout.main = mainRect

	case uiLanding:
		// Layout
		//
		// header
		// ------
		// main
		// ------
		// editor
		// ------
		// help
		var headerRect, mainRect image.Rectangle
		layout.Vertical(
			layout.Len(headerHeight),
			layout.Fill(1),
		).Split(appRect).Assign(&headerRect, &mainRect)
		var editorRect image.Rectangle
		layout.Vertical(
			layout.Len(mainRect.Dy()-editorHeight),
			layout.Fill(1),
		).Split(mainRect).Assign(&mainRect, &editorRect)
		// Remove extra padding from editor (but keep it for header and main)
		editorRect.Min.X -= 1
		editorRect.Max.X += 1
		uiLayout.header = headerRect
		uiLayout.main = mainRect
		uiLayout.editor = editorRect

	case uiChat:
		if m.isCompact {
			// Layout
			//
			// compact-header
			// ------
			// main
			// ------
			// editor
			// ------
			// help
			const compactHeaderHeight = 1
			var headerRect, mainRect image.Rectangle
			layout.Vertical(
				layout.Len(compactHeaderHeight),
				layout.Fill(1),
			).Split(appRect).Assign(&headerRect, &mainRect)
			detailsHeight := min(sessionDetailsMaxHeight, area.Dy()-1) // One row for the header
			var sessionDetailsArea image.Rectangle
			layout.Vertical(
				layout.Len(detailsHeight),
				layout.Fill(1),
			).Split(appRect).Assign(&sessionDetailsArea, new(image.Rectangle))
			uiLayout.sessionDetails = sessionDetailsArea
			uiLayout.sessionDetails.Min.Y += compactHeaderHeight // adjust for header
			// Add one line gap between header and main content
			mainRect.Min.Y += 1
			var editorRect image.Rectangle
			layout.Vertical(
				layout.Len(mainRect.Dy()-editorHeight),
				layout.Fill(1),
			).Split(mainRect).Assign(&mainRect, &editorRect)
			mainRect.Max.X -= 1 // Add padding right
			uiLayout.header = headerRect
			pillsHeight := m.pillsAreaHeight()
			if pillsHeight > 0 {
				pillsHeight = min(pillsHeight, mainRect.Dy())
				var chatRect, pillsRect image.Rectangle
				layout.Vertical(
					layout.Len(mainRect.Dy()-pillsHeight),
					layout.Fill(1),
				).Split(mainRect).Assign(&chatRect, &pillsRect)
				uiLayout.main = chatRect
				uiLayout.pills = pillsRect
			} else {
				uiLayout.main = mainRect
			}
			// Add bottom margin to main
			uiLayout.main.Max.Y -= 1
			uiLayout.editor = editorRect
		} else {
			// Layout
			//
			// ------|---
			// main  |
			// ------| side
			// editor|
			// ----------
			// help

			var headerRect, bodyRect image.Rectangle
			layout.Vertical(
				layout.Len(headerHeight),
				layout.Fill(1),
			).Split(appRect).Assign(&headerRect, &bodyRect)
			var mainRect, sideRect image.Rectangle
			layout.Horizontal(
				layout.Len(bodyRect.Dx()-sidebarWidth),
				layout.Fill(1),
			).Split(bodyRect).Assign(&mainRect, &sideRect)
			uiLayout.header = headerRect
			// Add padding left
			sideRect.Min.X += 1
			var editorRect image.Rectangle
			layout.Vertical(
				layout.Len(mainRect.Dy()-editorHeight),
				layout.Fill(1),
			).Split(mainRect).Assign(&mainRect, &editorRect)
			mainRect.Max.X -= 1 // Add padding right
			uiLayout.sidebar = sideRect
			pillsHeight := m.pillsAreaHeight()
			if pillsHeight > 0 {
				pillsHeight = min(pillsHeight, mainRect.Dy())
				var chatRect, pillsRect image.Rectangle
				layout.Vertical(
					layout.Len(mainRect.Dy()-pillsHeight),
					layout.Fill(1),
				).Split(mainRect).Assign(&chatRect, &pillsRect)
				uiLayout.main = chatRect
				uiLayout.pills = pillsRect
			} else {
				uiLayout.main = mainRect
			}
			// Add bottom margin to main
			uiLayout.main.Max.Y -= 1
			uiLayout.editor = editorRect
		}
	}

	return uiLayout
}
