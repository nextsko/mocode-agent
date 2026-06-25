---
name: pptx
description: >-
  Use when the user needs to create, edit, or process Microsoft PowerPoint
  (.pptx) presentations. Covers generating slides from data, adding charts,
  tables, images, animations, transitions, speaker notes, slide layouts,
  master slides, converting Markdown to PPTX, merging presentations,
  extracting content, and batch slide creation. Uses python-pptx or
  LibreOffice depending on the task.
---

# PPTX — Microsoft PowerPoint Presentations

Create, edit, and generate presentations programmatically.

## Quick Start

```bash
# Install
pip install python-pptx

# Simple example
python << 'EOF'
from pptx import Presentation
from pptx.util import Inches, Pt

prs = Presentation()
slide = prs.slides.add_slide(prs.slide_layouts[0])  # Title slide
title = slide.shapes.title
title.text = "My Presentation"
subtitle = slide.placeholders[1]
subtitle.text = "Generated with python-pptx"
prs.save("output.pptx")
EOF
```

## Common Tasks

### 1. Creating Slides with Different Layouts

```python
from pptx import Presentation
from pptx.util import Inches, Pt
from pptx.enum.text import PP_ALIGN

prs = Presentation()
# Available layouts: 0=Title, 1=Title+Content, 2=Section, 3=Two Content, etc.

# Title slide (layout 0)
slide = prs.slides.add_slide(prs.slide_layouts[0])
slide.shapes.title.text = "Annual Report 2025"
slide.placeholders[1].text = "Presented by Data Team"

# Content slide (layout 1)
slide2 = prs.slides.add_slide(prs.slide_layouts[1])
slide2.shapes.title.text = "Q1 Results"
content = slide2.placeholders[1].text_frame
content.text = "Revenue grew 23% YoY"

# Add bullet points
p = content.add_paragraph()
p.text = "Costs reduced by 15%"
p.level = 0
p = content.add_paragraph()
p.text = "New markets: APAC, EU"
p.level = 1  # sub-bullet

prs.save("presentation.pptx")
```

### 2. Adding Tables

```python
from pptx import Presentation
from pptx.util import Inches

prs = Presentation()
slide = prs.slides.add_slide(prs.slide_layouts[5])  # Blank layout

# Create table
rows, cols = 4, 3
table_shape = slide.shapes.add_table(rows, cols, Inches(1), Inches(1), Inches(8), Inches(3))
table = table_shape.table

# Set column widths
table.columns[0].width = Inches(3)
table.columns[1].width = Inches(2.5)
table.columns[2].width = Inches(2.5)

# Header
headers = ["Metric", "Q1 2025", "Q1 2024"]
for i, h in enumerate(headers):
    cell = table.cell(0, i)
    cell.text = h
    # Bold header
    for p in cell.text_frame.paragraphs:
        p.font.bold = True
        p.font.size = Pt(12)

# Data
data = [
    ["Revenue", "$1.2M", "$980K"],
    ["Users", "50K", "38K"],
    ["Growth", "23%", "18%"],
]
for row_idx, row_data in enumerate(data, 1):
    for col_idx, val in enumerate(row_data):
        table.cell(row_idx, col_idx).text = val

prs.save("table.pptx")
```

### 3. Adding Charts

```python
from pptx import Presentation
from pptx.chart.data import CategoryChartData
from pptx.enum.chart import XL_CHART_TYPE
from pptx.util import Inches

prs = Presentation()
slide = prs.slides.add_slide(prs.slide_layouts[5])

# Chart data
chart_data = CategoryChartData()
chart_data.categories = ['Q1', 'Q2', 'Q3', 'Q4']
chart_data.add_series('Revenue', (100, 120, 115, 150))
chart_data.add_series('Costs', (60, 65, 70, 80))

# Add chart
chart_shape = slide.shapes.add_chart(
    XL_CHART_TYPE.COLUMN_CLUSTERED,
    Inches(1), Inches(1), Inches(8), Inches(5),
    chart_data
)
chart = chart_shape.chart
chart.has_legend = True

# For pie chart, use:
# XL_CHART_TYPE.PIE
# For line chart, use:
# XL_CHART_TYPE.LINE_MARKERS

prs.save("chart.pptx")
```

### 4. Images & Shapes

```python
from pptx import Presentation
from pptx.util import Inches
from pptx.enum.shapes import MSO_SHAPE

prs = Presentation()
slide = prs.slides.add_slide(prs.slide_layouts[5])

# Add image
slide.shapes.add_picture("chart.png", Inches(1), Inches(1), Inches(4), Inches(3))

# Add shape (rectangle)
shape = slide.shapes.add_shape(
    MSO_SHAPE.RECTANGLE,
    Inches(6), Inches(1), Inches(3), Inches(1)
)
shape.text = "Important Note"
shape.fill.solid()
shape.fill.fore_color.rgb = RGBColor(0x00, 0x70, 0xC0)

# Add rounded rectangle
shape2 = slide.shapes.add_shape(
    MSO_SHAPE.ROUNDED_RECTANGLE,
    Inches(6), Inches(3), Inches(3), Inches(1)
)
shape2.text = "Key Insight"

prs.save("images.pptx")
```

### 5. Speaker Notes

```python
from pptx import Presentation

prs = Presentation("presentation.pptx")
for slide in prs.slides:
    # Add speaker notes
    notes_slide = slide.notes_slide
    notes_slide.notes_text_frame.text = "Key talking points for this slide"

# Or access existing notes
for slide in prs.slides:
    if slide.has_notes_slide:
        notes = slide.notes_slide.notes_text_frame.text
        print(f"Slide {prs.slides.index(slide) + 1}: {notes}")

prs.save("with_notes.pptx")
```

### 6. Slide Transitions

```python
from pptx.oxml.ns import qn
from pptx import Presentation

prs = Presentation("input.pptx")
for slide in prs.slides:
    transition = slide._element.makeelement(qn('p:transition'), {})
    transition.set(qn('p:transitionType'), 'fade')
    # Options: cut, fade, push, wipe, split, blinds, etc.
    slide._element.append(transition)

prs.save("transition.pptx")
```

### 7. Converting MD to PPTX

```bash
# Using pandoc with reveal.js (HTML slides)
pandoc slides.md -t revealjs -o slides.html

# Convert HTML to PPTX via LibreOffice
# Or write python script to parse markdown sections into slides

# Simple MD to PPTX with python
python << 'EOF'
from pptx import Presentation
import re

prs = Presentation()
with open("slides.md", "r") as f:
    content = f.read()

# Split by h1/h2 headings
slides = re.split(r'^# |^## ', content, flags=re.MULTILINE)
for slide_text in slides:
    if not slide_text.strip():
        continue
    lines = slide_text.strip().split('\n')
    slide = prs.slides.add_slide(prs.slide_layouts[1])
    slide.shapes.title.text = lines[0]
    if len(lines) > 1:
        tf = slide.placeholders[1].text_frame
        for line in lines[1:]:
            if line.strip():
                p = tf.add_paragraph()
                p.text = line.strip()
                p.font.size = Pt(18)

prs.save("from_md.pptx")
EOF
```

### 8. Batch from Data (JSON → Slides)

```python
import json
from pptx import Presentation
from pptx.util import Inches

with open("data.json") as f:
    data = json.load(f)

prs = Presentation()
for item in data:
    slide = prs.slides.add_slide(prs.slide_layouts[1])
    slide.shapes.title.text = item["title"]
    tf = slide.placeholders[1].text_frame
    for point in item["bullets"]:
        p = tf.add_paragraph()
        p.text = point
        p.font.size = Pt(16)

prs.save("batch.pptx")
```

### 9. Extract Text from PPTX

```python
from pptx import Presentation

prs = Presentation("input.pptx")
for i, slide in enumerate(prs.slides, 1):
    print(f"\n=== Slide {i} ===")
    for shape in slide.shapes:
        if hasattr(shape, "text"):
            print(shape.text)
```

## Install Dependencies

```bash
pip install python-pptx pandoc
```

## Verification

```bash
# Check generated file
python -c "from pptx import Presentation; prs = Presentation('output.pptx'); print(f'{len(prs.slides)} slides')"
```
