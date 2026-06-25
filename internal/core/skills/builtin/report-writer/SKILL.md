---
name: report-writer
description: >-
  Use when the user needs to write structured reports — data analysis reports,
  weekly/monthly reports, industry research reports, project status reports,
  academic summaries, or any formal document requiring a professional template.
  Covers report structure design, data-driven writing, insight extraction, and
  formatted output (.md, .html, .docx, .pdf). NOT for creative writing, code
  documentation, or simple email replies.
---

# Report Writer

Professional report writing with structured templates.

## Core Principles

1. **Insight First** — Don't just list facts; extract trends, anomalies, and actionable recommendations
2. **Data-Driven** — Every conclusion must be backed by data or findings
3. **Structure** — Executive summary → Analysis → Conclusions → Recommendations
4. **Concision** — Can the reader grasp the core message in 2 minutes?
5. **File Output** — Always write the report to a file, never just in chat

## Available Templates

### 1. Data Analysis Report

Best for: sales analysis, operational metrics, performance reviews, survey results.

```markdown
# [Title] — Data Analysis Report

## Executive Summary
[3-5 sentences: key findings + top recommendation]

## Analysis

### Overall Trends
[Big picture trends with key numbers]

### Dimension 1: [Name]
[Breakdown with supporting data]

### Dimension 2: [Name]
[Breakdown with supporting data]

### Key Metrics
| Metric | Current | Previous | Change | Benchmark | Status |
|--------|---------|----------|--------|-----------|--------|
|        |         |          |        |           |   ✅/⚠️/❌ |

## Key Findings
1. **[Finding 1]** — [data support, impact]
2. **[Finding 2]** — [data support, impact]
3. **[Finding 3]** — [data support, impact]

## Recommendations
| Priority | Action | Rationale | Expected Impact |
|----------|--------|-----------|-----------------|
| P0       |        |           |                 |
| P1       |        |           |                 |

## Appendix
- Data sources
- Methodology
- Limitations
```

### 2. Weekly / Monthly Report

Best for: personal weekly, team status, project updates, management reports.

```markdown
# [Weekly/Monthly] Report — [Team/Person]
> [Date Range]

## Highlights
- ✅ [Key achievement 1]
- ✅ [Key achievement 2]
- 🚧 [Ongoing item]

## Completed
| Task | Impact | Notes |
|------|--------|-------|
|      |        |       |

## In Progress
| Task | Progress | ETA | Blockers |
|------|----------|-----|----------|
|      | XX%      |     |          |

## Metrics Dashboard
| Metric | This Period | Last Period | Change | Goal | Status |
|--------|-------------|-------------|--------|------|--------|
|        |             |             |        |      |        |

## Next Week Plan
1. **[Task 1]** — [why it matters]
2. **[Task 2]** — [why it matters]
3. **[Task 3]** — [why it matters]

## Risks & Mitigation
| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
|      | H/M/L     | H/M/L  |            |
```

### 3. Industry Research Report

Best for: competitive analysis, market research, technology landscape studies.

```markdown
# [Topic] — Industry Research Report

## Executive Summary

## Market Overview
- **Market Size**: [value, growth rate]
- **Key Players**: [list]
- **Trends**: [3-5 key trends]

## Deep Dive
### Aspect 1: [Name]
[Analysis with evidence, data, citations]

### Aspect 2: [Name]
[Analysis with evidence, data, citations]

## Competitive Landscape
| Company | Strengths | Weaknesses | Strategy | Market Share |
|---------|-----------|------------|----------|-------------|
|         |           |            |          |             |

## Conclusions
[3-5 key takeaways]

## Recommendations
1. **[Action]** — [rationale]
```

### 4. Project Status Report

```markdown
# Project Status — [Project Name]
**Period**: [Date] | **Status**: 🟢 On Track / 🟡 At Risk / 🔴 Blocked

## Progress Summary
- **Overall**: XX%
- **This Period**: [completed items]
- **Next Milestone**: [name, date]

## Milestones
| Milestone | Planned | Actual | Status | Notes |
|-----------|---------|--------|--------|-------|
|           |         |        | ✅/❌  |       |

## Risks
| Risk | Probability | Impact | Mitigation | Owner |
|------|-----------|--------|------------|-------|
|      | H/M/L     | H/M/L  |            |       |

## Action Items
| Action | Owner | Due | Status |
|--------|-------|-----|--------|
|        |       |     |        |
```

## Writing Guidelines

### Do ✅
- Lead with the most important finding
- Use specific numbers: "revenue grew 23%" not "revenue grew significantly"
- One key insight per paragraph
- Use tables for multi-dimensional data
- End each section with a takeaway
- Use Mermaid diagrams for workflows

### Don't ❌
- No filler phrases: "it is worth noting that", "as previously mentioned"
- No vague statements without data
- Don't hide problems — present risks alongside achievements
- Don't write paragraphs where a table would work better
- No subjective adjectives without evidence

## Output Formats

```bash
# Default: Markdown
pandoc report.md -o report.html           # HTML with styling
pandoc report.md -o report.docx           # Word document
pandoc report.md --pdf-engine=weasyprint -o report.pdf  # PDF

# With table of contents
pandoc report.md --toc -o report.html
```

## Verification Checklist

- [ ] Executive summary captures all key points
- [ ] Every claim has supporting data
- [ ] Tables are formatted and readable
- [ ] Recommendations are actionable and prioritized
- [ ] File is saved to disk (not just in chat)
- [ ] Format matches user's requirements (.md/.html/.docx/.pdf)
