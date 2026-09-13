---
name: ecommerce-image-studio
description: >-
  Use when 用户想把产品照片变成可直接上架的电商图组：listing 主图、生活场景图、广告首帧、
  卖点图、细节图、模特/lookbook。给出平台化创作方向、默认六图组、creative archetype、
  模特图策略、提示词结构与保真/合规失败处理；本 skill 只做电商策略层，实际生图/改图交给
  底层图像生成执行层。
---

# Ecommerce Image Studio — 产品图 → 电商图组

把一张产品图扩展成一套 market-ready 的电商视觉，而不逼用户自己写提示词。
本 skill 是**电商策略层**：决定方向、写 creative brief、产出每条提示词；实际生成/编辑交给图像生成执行层（如 `gpt-image-2` / `oo` CLI）。

相关：`design-taste-frontend`（审美与风格决策）、`web-design-guidelines`（文字可读性与对比度）、`screenshot-to-ui`（生成素材落到页面时）。

## 何时用

- 一张产品图 → 一组 listing / 场景 / 广告首帧 / 卖点 / 细节图。
- 服饰、配饰、包袋、珠宝、美妆、可穿戴品的模特 / lookbook / 上身 / 携带 / 陈列图。
- 适配 Amazon、Shopify、TikTok Shop、小红书、抖音、淘宝/天猫、Shopee、Temu、Etsy、DTC。
- 让图更高级 / 更可点 / 更平台原生 / 更少 AI 味 / 更贴合目标市场。
- 同一张产品图产出多个创意方向做 A/B。

## 输入

- **必需**：≥1 张产品图。**保真**其形状、比例、颜色、材质观感、logo 位置、包装、标签、SKU 特征与独特物理细节。
- 可选：目标平台（缺省=通用电商）、市场/语言（缺省=用户语言 + 国际通用风格）、输出目标（缺省=均衡六图组）、卖点、模特偏好、参考图、宽高比、数量。

缺省宽高比：方图 `1024x1024`；社媒封面/竖版广告 `1024x1536`；官网 hero/banner `1536x1024`。

只在以下情况追问**一次**：无产品图、输出类型无法推断、要求特定真人 likeness 或精确试穿却没有模特/参考图、受监管或依赖证明的宣称会实质改变画面。其余情况选安全默认值直接做。

## 默认六图组

用户要“一套”时，除非另有说明，生成：

1. **Clean listing**：产品为主、平台安全、最小道具。
2. **Premium lifestyle scene**：真实使用语境 + 精致打光。
3. **Social ad first frame**：强吸睛构图 + 清晰 text-safe 留白。
4. **Selling-point graphic**：一个明确卖点/特性，移动端可读。
5. **Detail / texture**：材质、结构、工艺、包装或部件特写。
6. **Market variant**：第二创意方向做 A/B（如 UGC-real-life、premium-studio、giftable-scene、model-lookbook）。

服饰/配饰/珠宝/包袋/美妆等可穿戴品：整套里至少含 1 张模特 / lookbook / 上身图，除非用户明确要无模特。六图时把模特图放第 2 或第 6 张（取更有商业价值的位置）。

## 平台方向

- **Amazon**：干净、可信、产品优先；无未证实宣称、无假徽章、文字不过量。
- **Shopify / DTC**：品牌感、编辑感、会讲故事。
- **TikTok Shop / Meta ads**：真实、快读、UGC 能量、首帧强对比。
- **小红书**：生活主导、有品味、封面友好；需要中文标题时留干净空间。
- **抖音 / 淘宝 / 天猫 / 京东 / 拼多多**：可接受更高转化密度，但文字可读、宣称属实。
- **Shopee / Temu**：高对比、卖点层级简单、小尺寸下产品清晰。
- **Etsy**：手作、礼物、温暖、材质感知；除要求外避免过度商业化。

## 创作原型（Creative Archetypes）

从中选一个或多个，别让用户自己发明提示词：
`clean-marketplace` / `premium-studio` / `ugc-real-life` / `social-first-frame` / `feature-callout` / `macro-texture` / `giftable-scene` / `problem-solution` / `model-lookbook`。

## 模特图策略

模特图传达比例、搭配、垂坠、场景与向往感，默认**不要**从服饰套图里删掉。

- 图像编辑模式把**产品图放第一张**；有模特/姿态/生活参考时放其后，仅用于姿态、裁剪、打光、构图与氛围。
- 未经授权不得复制真人身份，除非用户拥有或提供了该参考并明确要求保留。
- 无模特偏好时给一个市场适配的通用方向：
  - 度假风 / 连衣裙 / 罩衫：暖色度假 lookbook，阳光、站姿或行走，全身或四分之三。
  - 珠宝 / 包袋 / 腕表 / 配饰：局部或手/肩裁剪，产品清晰，非必要不露脸。
  - 美妆个护：有品味的特写或浴室/梳妆台场景，避免医疗宣称或假前后对比。
- 提示词里要诚实：做**商业模特/生活图**，不承诺精确试穿；尽量保留印花、颜色、廓形，但不宣称精确版型/尺码/工艺。

## 提示词结构

每条输出图一条聚焦提示词：

```text
Create a market-ready ecommerce image for <platform/market>.
Primary product: preserve the product from image 1 accurately, including shape,
proportions, color, material appearance, packaging, labels, logo placement,
SKU details, and distinctive physical features.
Image type: <listing/lifestyle/ad first frame/selling point/detail>.
Creative direction: <archetype + 简短理由>.
Scene and composition: <场景、机位、产品大小、裁剪、text-safe 留白>.
Lighting and style: <真实打光、配色、质感、市场调性>.
Visible text: <none 或目标语言的简短事实文案>；移动端可读，避免过小字体。
Reference usage: images 2+ 仅用于 mood/scene/composition/lighting/crop/layout 灵感；
不要复制无关产品、竞品品牌、水印、徽章或精确版式。
Avoid: distorted product, changed logo, changed packaging, fake certifications,
fake platform badges, fake discounts, unsupported performance claims, QR codes,
phone numbers, URLs, unreadable text, obvious AI gloss, plastic skin, warped hands.
```

模特 lookbook 追加：

```text
Model direction: <目标市场的通用模特、姿态、裁剪、造型氛围>；产品自然穿戴、清晰可见、商业上好看。
Accuracy: 保留 image 1 的服装/产品颜色、印花、廓形、细节与比例印象；不宣称精确版型/尺码/塑形。
Avoid: 未授权的真人 likeness、畸形解剖、扭曲的手、改变的印花/领口/袖口/下摆、
过度性感姿势、虚假身材效果、塑料皮肤、产品被遮挡。
```

## 提交策略

- 默认六图组：**并行**提交所有独立任务，每张图一条提示词、一次编辑调用、一个确定性输出名。
- 不要塌缩成一次 `n=6`，除非用户明确要同一概念的近似变体。
- 更大套图 / 含大量文字 / 严苛模特试穿 / 极高保真：把并发降到 3–4。
- 单张失败（provider busy、超时、上传、瞬时连接错误）：检查结果状态后**只重试失败的那张一次**，不要重投成功项。

## 结果与命名

按角色命名：`listing-01.png`、`lifestyle-01.png`、`ad-first-frame-01.png`、`selling-point-01.png`、`detail-01.png`、`variant-01.png`。

生成后简短汇报：用了哪个平台/市场方向、产出了哪些图、有什么取舍或安全调整、一条下一步迭代建议（更强 UGC 真实感 / 更干净的平台合规 / 更高级造型）。

## 失败处理

- **缺产品图**：要一张，停止。
- **目标含糊**：默认六图组，除非用户明确要单张。
- **缺平台**：默认通用电商，并说明可再出平台专属变体。
- **未证实/高风险宣称**：删除或索要证据；不得编造认证、医疗、安全、性能数字、奖项、保修或绝对化宣称。
- **文字过多**：图上文字保持简短；复杂详情页建议先生成干净背景图，再用设计工具排版文字。
- **保真风险**：减少编辑、干净背景、产品主导构图；模特改动了产品就加严保真提示词重跑。
- **参考图风险**：只当 mood/layout 用；绝不复制竞品包装、商标、水印或标志性 campaign 版式。

## 反模式

- 不写提示词就让执行层自由发挥；把一套不同用途的图塞进一次 `n=6`。
- 复制竞品品牌/水印/徽章/精确版式；编造认证、折扣、性能数字。
- 用精确试穿/精确尺码/塑形效果等无法兑现的承诺。
- 服饰套图默认删掉模特图。
