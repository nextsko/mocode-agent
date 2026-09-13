---
name: kami-pdf-workflow
description: >-
  Use when 需要用 Kami 设计系统（tw93/Kami）生成印刷级 PDF 文档——「生成 PDF / 排版 /
  一页纸 / 个人画像 / profile PDF / HTML 转 PDF / 简历 / 白皮书 / 信件 / 作品集 / 幻灯片」，
  即填充 Kami HTML 模板并用 Playwright Chromium 渲染成 A4 PDF 时。
---

# kami-pdf-workflow：用 Kami 生成印刷级 PDF

## 概述

使用 **Kami** 设计系统（`tw93/Kami`）生成印刷级 PDF：选模板 → 填 `{{PLACEHOLDER}}` → 用 Playwright Chromium 渲染成 A4。适用于一页纸、简历、白皮书、信件、作品集、幻灯片等固定版式文档；需要可打印、可外发的正式产物时使用。内容与结构的写法另见 `report-writer`——本技能只负责版式与渲染。

## 前置条件

1. **Kami skill 已安装**：

   ```bash
   npx skills add tw93/kami/plugins/kami -a universal -g -y
   ```

   安装位置：`~/.agents/skills/kami/`。

2. **Playwright 已安装**（Node 环境）：

   ```bash
   mkdir -p ~/.workbuddy/binaries/node/workspace
   cd ~/.workbuddy/binaries/node/workspace
   npm init -y
   npm install playwright
   npx playwright install chromium
   ```

## 工作流

### Step 1 选模板

Kami 的 CN 模板位于 `~/.agents/skills/kami/assets/templates/`：

| 文档类型 | 模板文件 |
|----------|----------|
| 一页纸 / 方案 | `one-pager.html` |
| 简历 | `resume.html` |
| 长文档 / 白皮书 | `long-doc.html` |
| 信件 | `letter.html` |
| 作品集 | `portfolio.html` |
| 幻灯片 | `slides-weasy.html` |

### Step 2 填内容

复制模板到工作目录，替换全部 `{{PLACEHOLDER}}`：

```bash
cp ~/.agents/skills/kami/assets/templates/one-pager.html ./my-doc.html
```

遵循 Kami 设计规范：

- 底色 `#f5f4ed`（羊皮纸），**非纯白**。
- 强调色 `#1B365D`（墨蓝），**全文档唯一强调色**。
- 中文字体 TsangerJinKai02（CDN 回退）。
- 所有灰色为暖色调（黄棕底）。

### Step 3 用 Playwright 渲染 PDF

```javascript
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({
    viewport: { width: 794, height: 1123 },
    deviceScaleFactor: 2
  });
  await page.goto('file:///absolute/path/to/doc.html', {
    waitUntil: 'networkidle',
    timeout: 30000
  });
  await page.pdf({
    path: '/absolute/path/to/output.pdf',
    format: 'A4',
    margin: { top: '0mm', right: '0mm', bottom: '0mm', left: '0mm' },
    printBackground: true,
    preferCSSPageSize: true
  });
  await browser.close();
  console.log('PDF generated');
})().catch(e => { console.error(e); });
```

`goto` 用**绝对** `file://` 路径；PDF 输出同样用绝对路径。

### Step 4 关键配置

| 参数 | 作用 |
|------|------|
| `deviceScaleFactor: 2` | 高清渲染，文字更锐利 |
| `printBackground: true` | 打印羊皮纸背景色 |
| `preferCSSPageSize: true` | 采用 CSS `@page` 定义的 A4 尺寸 |
| `waitUntil: 'networkidle'` | 等待字体 CDN 加载完成 |

## Fallback

Windows 上 WeasyPrint 需要 GTK3 运行时。无法安装 GTK3 时，用 **Playwright Chromium 替代 WeasyPrint**，不要为此在系统里硬装 GTK3。

## 验证清单

- [ ] 页面背景为 `#f5f4ed` 羊皮纸色
- [ ] 中文字体渲染正确（非系统回退字体）
- [ ] 一页以内完整展示（无溢出 / 截断）
- [ ] 墨蓝强调色占页面面积 ≤5%
- [ ] 所有链接 / 引用可用
- [ ] 已实际打开 PDF 目视核对（不是只看脚本跑通）

## 相关技能

- `report-writer`：报告内容与结构（本技能只负责版式与渲染）。
- `docs-rulebook`：PDF 之外的文档目录与生命周期纪律。
