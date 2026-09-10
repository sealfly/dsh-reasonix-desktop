# Logo 布局记录（DSH-Reasonix 字标）

> 此文件记录 logo SVG 的最终正确布局，防止将来误改。修改 logo 前必读。

## 🆕 2026-09-10：品牌 logo 更换为豆包设计稿（位图版）

作者提供新 logo（`SH-Reasonix_去水印.png`，2048×683，白底）：**左侧图标本身就是字母 D 的造型
（蓝色圆角方形 + 白色留白成 D 形 + 黑色虎鲸），右侧 "SH-Reasonix" 蓝色粗体**——
图标 + 文字整体读作 **DSH-Reasonix**（不是少写了 D）。

- 原始附件留存：`~/.dsh/attachments/v1/objects/83/8302e4d8…`（sha256:8302e4d8…）
- 处理脚本与素材：`%TEMP%\logo-prep.ps1`（白底抠透明 + 去白边 + 切分 + 缩放）→
  `%TEMP%\logo-prep\`（`mark-256.png`/`mark-128.png`/`wordmark-1024.png`）
- 替换脚本：`%TEMP%\apply-logo.ps1`（含 dist 原文件备份 `%TEMP%\logo-backup-<时间戳>\`）
- 落地形式：位图 PNG 以 **base64 内嵌进 SVG 包装**（保持原文件名，JS 引用不变）：
  - `frontend/dist/assets/logo-wordmark-0KJq8oA3.svg` ← wordmark 1024×131（侧边栏左上角 + 欢迎页）
  - `frontend/dist/assets/logo-C8rTDnTH.svg` ← 方形 mark 256×256（引导页）
  - `frontend/dist/index.html` boot-shell `<img src>` ← mark PNG data-URI（加载页 56×56）
  - `frontend/dist/assets/index-*.js` startup-splash 内联图标 ← mark PNG data-URI（启动闪屏 64×64）
- **CSS 覆盖**（注入在 `index.html` 的内联 `<style>` 里）：原 `.sidebar__brand-logo` 带
  `filter:brightness(0)invert()`（为单色 logo 反白用），会把彩色位图变成纯白剪影——
  已覆盖为 `filter:none!important`，并把尺寸/负边距按新比例适配
  （150×34、workbench 132×26、`margin-left:0`）。
- 旧版单色 SVG logo 的字母路径布局记录见下方（历史存档；若需回退可用
  `%TEMP%\logo-backup-*` 恢复）。

## 🟠 2026-09-10 追加：深色主题专用橙色版

深色背景（#111214）上蓝色 logo 对比度偏中等，故按作者要求加**橙色版**——用 **Reasonix 品牌亮橙
`#FF5A2C`**（实证依据：`frontend/dist/assets/styles-*.css` 里 `--accent:#ff5a2c`、`--accent-strong:#df471f`
及 `--sidebar-active/--workspace-selection-*` 系列均用该值，是 UI 强调色/选中态用色；
另注：`#f59e0b` 是 `--warn` 语义色，**不是**品牌色，勿混用）：

- 生成：`%TEMP%\logo-orange.ps1`——把蓝色像素映射为橙色（判据 `(B-R)/255` 的 blueness，
  by blueness 加权混合，**不做亮度压缩**以免变暗；黑鲸鱼与白色 D 形留白保持不变，
  抗锯齿过渡由 alpha 承载）→ `%TEMP%\logo-prep\*-dark-*.png`（采样验证 = R255 G90 B44 ✓）
- 落地：
  - **恒深色的位置直接换橙色**：`index.html` boot-shell 加载图标、两个 `index-*.js` 的
    startup-splash 内联图标（PNG data-URI 换成橙色 mark）
  - **随主题切换的位置**：新增 `frontend/dist/assets/logo-wordmark-dark.svg`（橙色字标）与
    `logo-mark-dark.svg`（橙色方形），在 `index.html` 内联 `<style>` 里按主题切换：
    ```css
    :root[data-theme=dark] .sidebar__brand-logo,:root:not([data-theme]) .sidebar__brand-logo,
    :root[data-theme=dark] .welcome__brand-logo,:root:not([data-theme]) .welcome__brand-logo{content:url("./assets/logo-wordmark-dark.svg")}
    :root[data-theme=dark] .onboarding__logo,:root:not([data-theme]) .onboarding__logo{content:url("./assets/logo-mark-dark.svg")}
    ```
    （浅色主题 `data-theme=light` 不匹配 → 仍用蓝色版 `logo-wordmark-0KJq8oA3.svg`）
- 备份：`%TEMP%\logo-backup-orange-<时间戳>\`（替换前的 index.html 与两个 index-*.js）
- ⚠️ 注意：`frontend/dist/` 整体在 `.gitignore` 里，**新增的 dist 资源必须 `git add -f`** 才能入库
- 迭代记录：初版用 `#E58A3A`（加载点同色）→ 作者反馈偏暗 → 改为品牌亮橙 `#FF5A2C` ✓

---

## （历史）单色 SVG 字标布局

> ## ⛔ 升级规则（重要）：logo 类文件不参与官方对照更新
>
> 对照 Reasonix 官方源码升级前端时，**以下文件一律保留本地定制版本，不参与官方对照/替换**：
> - `frontend/dist/assets/logo-wordmark-*.svg`（字标，本地布局见下）
> - `frontend/dist/assets/logo-C8rTDnTH.svg`（方形图标）
> - `frontend/dist/index.html` 中的 **boot-shell 内联 SVG**（加载页 logo，aria-label="DSH-Reasonix"）
> - `frontend/dist/index.html` 中的 **boot-shell 名称**（`boot-shell__name`=DSH-Reasonix）
> - `frontend/dist/index.html` 内联 `<style>` 里的 **logo 覆盖 CSS**（filter:none + 尺寸适配）
>
> 升级流程：官方 dist 覆盖后，**从旧 dist 备份恢复以上 logo/品牌内容**（文件名/内容均为本地定制版）。
> 备份位置参考：`$env:TEMP\dsh-dist-backup-v1290`（v1.29.0 定制版，含 DSH boot SVG 550 字节）。

## 字母顺序（唯一正确）

**DSH-** 前缀 + **R-e-a-s-o-n-i-x**（拼写为 "DSH-Reasonix"）

## 字形识别（关键！勿再认错）

| 字母 | 形状特征 | wordmark 路径 | square 路径 |
|---|---|---|---|
| R | 带腿+圈的 R 形 | M365.68（在 translate(123,-919.9) scale(1.254) 组内） | M365.68 |
| e | 横线+圆圈 | M767.26 | M575.08 |
| a | 双碗（两个圈） | M573.23 | M458.55 |
| s | **S 形波浪**（不是 i！） | M873.65 | M638.97 |
| o | **同心双圆**（不是 s！） | M1058.84 | M750.19 |
| n | **拱形**（不是 o！） | M1144.36 | M801.56 |
| i | 竖线 rect + 圆点 circle | rect x1181.54 + circle cx1250.95 | rect x823.88 + circle cx865.58 |
| x | **交叉斜线**（不是 n！） | M1430.09 | M973.16 |

**历史教训**：曾把 s/o/n/x 认错（S波→i、双圆→s、拱形→o、交叉→n），导致"重排"后 sonix 全乱。以本表为准。

## 当前布局参数

### wordmark（logo-wordmark-0KJq8oA3.svg）
- viewBox：`0 0 2520 392.25`
- 外层组：`<g transform="translate(385,0)">`
- 每个字母包 `<g transform="translate(tx,0)">`，均匀 210px 中心间距，R 中心在 x=950
- tx 值：R=8、e=81、a=446、s=354、o=420、n=472、i=575、x=679
- 字母最终中心：R950 e1160 a1370 s1580 o1790 n2000 i2211 x2420

### square（logo-C8rTDnTH.svg）
- viewBox：`0 0 1254 1254`
- 整行文本包 `<g transform="translate(-16,276) scale(0.7)">`
- 字母 tx（组内，20px 均匀间隙）：R=183、e=116、a=358、s=349、o=503、n=597、i=703、x=747
- 整行居中（x 范围 67-1188，中心 ~627 ≈ viewBox 中心）

## 验证方法

- getBBox()：对每个字母的 `<g>` 调用，返回**不含自身 transform** 的局部 bbox；最终位置 = 祖先 transform + tx + bbox
- 浏览器渲染 + 蓝色像素密度统计（Edge headless --screenshot）
