---
name: docx
description: >-
  Use when the user needs to create, edit, or process Microsoft Word (.docx)
  documents. Covers generating new documents from Markdown, editing text and
  formatting, inserting images/tables/headers/footers, managing comments and
  tracked changes, extracting text, converting between formats (MD to DOCX,
  DOCX to MD/pdf), modifying styles, replacing text, and handling document
  templates. Uses python-docx, pandoc, or LibreOffice depending on the task.
---

# DOCX — Microsoft Word Document Processing

Create, read, modify, and convert Word documents programmatically.

## Quick Start

```bash
# Convert Markdown to DOCX (simplest approach)
pandoc input.md -o output.docx

# Or use python-docx for complex operations
pip install python-docx
python << 'EOF'
from docx import Document
doc = Document()
doc.add_heading("Title", level=0)
doc.add_paragraph("Hello World")
doc.save("output.docx")
EOF
```

## Common Tasks

### 1. Create New Document

```python
from docx import Document
from docx.shared import Inches, Pt, Cm, RGBColor
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.enum.style import WD_STYLE_TYPE

doc = Document()

# Set default font
style = doc.styles['Normal']
font = style.font
font.name = 'Microsoft YaHei'
font.size = Pt(11)

# Heading levels 0-4
doc.add_heading("Document Title", level=0)  # Title
doc.add_heading("Section 1", level=1)        # Heading 1
doc.add_heading("Subsection", level=2)       # Heading 2

# Paragraph with formatting
p = doc.add_paragraph()
run = p.add_run("Bold and red text")
run.bold = True
run.font.color.rgb = RGBColor(0xFF, 0, 0)
run.font.size = Pt(14)

# Alignment
p.alignment = WD_ALIGN_PARAGRAPH.CENTER

# Lists
doc.add_paragraph("Item 1", style='List Bullet')
doc.add_paragraph("Item 2", style='List Number')

# Page break
doc.add_page_break()

doc.save("output.docx")
```

### 2. Edit Existing Document

```python
from docx import Document

doc = Document("existing.docx")

# Iterate paragraphs
for para in doc.paragraphs:
    if "old text" in para.text:
        for run in para.runs:
            if "old" in run.text:
                run.text = run.text.replace("old", "new")
                run.bold = True

# Iterate tables
for table in doc.tables:
    for row in table.rows:
        for cell in row.cells:
            print(cell.text)

doc.save("edited.docx")
```

### 3. Insert Images

```python
from docx import Document
from docx.shared import Inches

doc = Document()
doc.add_heading("Report with Image", level=1)

# Inline image
doc.add_picture("chart.png", width=Inches(5))

# Floating image (via table hack)
table = doc.add_table(rows=1, cols=1)
cell = table.cell(0, 0)
cell.paragraphs[0].alignment = WD_ALIGN_PARAGRAPH.CENTER
run = cell.paragraphs[0].add_run()
run.add_picture("logo.png", width=Inches(2))

doc.save("report.docx")
```

### 4. Tables

```python
from docx import Document
from docx.shared import Inches, Pt

doc = Document()

# Create table
table = doc.add_table(rows=3, cols=4, style='Table Grid')
table.autofit = True

# Header row
headers = ["Name", "Age", "Department", "Salary"]
for i, h in enumerate(headers):
    cell = table.rows[0].cells[i]
    cell.text = h
    # Bold header
    for p in cell.paragraphs:
        for r in p.runs:
            r.bold = True

# Data rows
data = [["Alice", "28", "Engineering", "100K"], ["Bob", "32", "Design", "90K"]]
for row_idx, row_data in enumerate(data, 1):
    for col_idx, val in enumerate(row_data):
        table.rows[row_idx].cells[col_idx].text = val

# Column width
for row in table.rows:
    row.cells[0].width = Inches(1.5)

doc.save("table.docx")
```

### 5. Headers & Footers

```python
from docx import Document
from docx.shared import Pt, Cm

doc = Document()

# Header
section = doc.sections[0]
header = section.header
header.is_linked_to_previous = False
hp = header.paragraphs[0]
hp.text = "Confidential"
hp.alignment = WD_ALIGN_PARAGRAPH.RIGHT
for run in hp.runs:
    run.font.size = Pt(8)
    run.font.color.rgb = RGBColor(0x99, 0x99, 0x99)

# Footer
footer = section.footer
footer.is_linked_to_previous = False
fp = footer.paragraphs[0]
fp.text = "Page "
# Add page number field
run = fp.add_run()
# Page number via XML
from docx.oxml.ns import qn
fldChar1 = run._r.makeelement(qn('w:fldChar'), {qn('w:fldCharType'): 'begin'})
run._r.append(fldChar1)
run2 = fp.add_run()
instrText = run2._r.makeelement(qn('w:instrText'), {})
instrText.text = 'PAGE'
run2._r.append(instrText)
fldChar2 = run._r.makeelement(qn('w:fldChar'), {qn('w:fldCharType'): 'end'})
run._r.append(fldChar2)

doc.save("with_header.docx")
```

### 6. Styles Management

```python
from docx import Document
from docx.shared import Pt, RGBColor
from docx.enum.style import WD_STYLE_TYPE

doc = Document()

# Modify existing style
heading_style = doc.styles['Heading 1']
heading_style.font.size = Pt(18)
heading_style.font.color.rgb = RGBColor(0x00, 0x70, 0xC0)
heading_style.font.bold = True

# Create custom style
new_style = doc.styles.add_style('Code Block', WD_STYLE_TYPE.PARAGRAPH)
new_style.font.name = 'Consolas'
new_style.font.size = Pt(9)
new_style.paragraph_format.space_before = Pt(6)
new_style.paragraph_format.space_after = Pt(6)

doc.add_heading("Styled Document", level=1)
doc.add_paragraph("print('hello')", style='Code Block')
doc.save("styled.docx")
```

### 7. Extract Text from DOCX

```bash
# Using pandoc (simplest)
pandoc input.docx -t plain -o output.txt

# Using python-docx
python << 'EOF'
from docx import Document
doc = Document("input.docx")
for para in doc.paragraphs:
    print(para.text)
EOF
```

### 8. Markdown to DOCX

```bash
# Basic conversion
pandoc input.md -o output.docx

# With reference docx for styling
pandoc input.md --reference-doc=template.docx -o output.docx

# With table of contents
pandoc input.md --toc -o output.docx
```

### 9. Comments & Tracked Changes

```python
from docx import Document
from docx.oxml.ns import qn
from docx.oxml import OxmlElement

doc = Document("draft.docx")

# Add comment to first paragraph
para = doc.paragraphs[0]
comment = OxmlElement('w:comment')
comment.set(qn('w:id'), '0')
comment.set(qn('w:author'), 'Reviewer')
comment.set(qn('w:date'), '2025-01-01T00:00:00Z')

# Comment text
comment_text = OxmlElement('w:p')
comment_run = OxmlElement('w:r')
comment_t = OxmlElement('w:t')
comment_t.text = 'Please revise this paragraph'
comment_t.set(qn('xml:space'), 'preserve')
comment_run.append(comment_t)
comment_text.append(comment_run)
comment.append(comment_text)

# Insert comment reference in paragraph
run = para.add_run()
comment_ref = OxmlElement('w:commentReference')
comment_ref.set(qn('w:id'), '0')
run._r.append(comment_ref)

# Add to document's comments part
# (Requires manipulating the document's XML directly)
doc.save("reviewed.docx")
```

## Reference Files

See reference docs in this skill directory for advanced patterns.

## Install Dependencies

```bash
pip install python-docx pandoc
# Or for Windows
choco install pandoc
```

## Verification

```bash
# Verify output
pandoc output.docx -t plain | head -20
# Check file
ls -la output.docx
```
