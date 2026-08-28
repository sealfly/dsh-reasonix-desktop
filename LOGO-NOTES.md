# Logo 布局记录（DSH-Reasonix 字标）

> 此文件记录 logo SVG 的最终正确布局，防止将来误改。修改 logo 前必读。

> ## ⛔ 升级规则（重要）：logo 类文件不参与官方对照更新
>
> 对照 Reasonix 官方源码升级前端时，**以下文件一律保留本地定制版本，不参与官方对照/替换**：
> - `frontend/dist/assets/logo-wordmark-*.svg`（字标，本地布局见下）
> - `frontend/dist/assets/logo-C8rTDnTH.svg`（方形图标）
> - `frontend/dist/index.html` 中的 **boot-shell 内联 SVG**（加载页 logo，aria-label="DSH-Reasonix"）
> - `frontend/dist/index.html` 中的 **boot-shell 名称**（`boot-shell__name`=DSH-Reasonix）
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
