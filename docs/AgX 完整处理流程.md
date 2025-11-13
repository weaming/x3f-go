AgX 完整处理流程

## 一、整体流程（6 个阶段）

```
RAW数据
  ↓
[1] RAW解码和预处理
  ↓ Intermediate [59-16383]
[2] 归一化
  ↓ RAW Linear [0.0-1.0]
[3] 色彩空间转换
  ↓ sRGB Linear [0.0-HDR]
[4] AgX Tone Mapping ← 核心步骤
  ↓ sRGB Linear [0.0-1.0]
[5] Gamma校正
  ↓ sRGB Gamma [0.0-1.0]
[6] JPEG输出
  ↓ 8-bit [0-255]
```

---

## 二、AgX Tone Mapping 详细步骤

Step 1: sRGB → E-Gamut

使用 FilmLight E-Gamut 色彩空间矩阵转换

- 输入: sRGB Linear [0.0-HDR]
- 输出: E-Gamut Linear [0.0-HDR]
- 目的: 转到宽色域，保留更多色彩信息

Step 2: Log2 编码

将线性数据转换为对数空间

- 动态范围: 25 stops (从 -12.47393 到 12.5260688117)
- 输入: E-Gamut Linear [0.0-HDR]
- 输出: Log-encoded [0.0-1.0]
- 目的: 压缩 HDR 动态范围到 [0,1] 区间

Step 3: 3D LUT 查找

使用三线性插值在 3D 查找表中查找映射值

- LUT 规格: 57×57×57 = 185,193 entries (6.4MB)
- 数据源: AgX_Base_sRGB.cube (Troy Sobotka)
- 插值方法: 三线性插值（8 个角点加权平均）
- 输入: Log-encoded [0.0-1.0]
- 输出: Rec.1886 encoded [0.0-1.0]

Step 4: Rec.1886 → Linear

移除 LUT 输出的 gamma 2.4 编码

- 输入: Rec.1886 encoded [0.0-1.0]
- 输出: sRGB Linear [0.0-1.0]
- 目的: 转回线性空间，由后续流程统一处理 gamma

---

## 三、关键概念解释

1. E-Gamut vs sRGB

- E-Gamut (FilmLight): 宽色域，比 sRGB 能表示更多颜色
- sRGB: 标准显示色域，最终输出格式
- 在宽色域中处理可避免中间过程裁剪颜色

2. Log2 编码的意义

- 25 stops = 2^25:1 的对比度范围（从极暗到极亮）
- 符合人眼对亮度的对数感知特性
- 在对数空间处理 tone mapping 更接近视觉感受

3. 为什么需要 3D LUT

- 简单数学公式难以完美模拟胶片的复杂响应曲线
- 3D LUT 可以精确控制每个颜色（RGB 三维）的映射关系
- Troy Sobotka 经过大量测试优化了这个 LUT
- 可以同时处理亮度和色彩的非线性变换

4. 为什么 Step 4 要转回线性

- 保持与 ACES/Filmic 输出格式一致
- 避免双重 gamma 校正（这是之前的 bug）
- 让整个 pipeline 在最后统一处理 gamma

---

## 四、三种 Tone Mapping 算法对比

| 算法   | 复杂度 | 核心原理                | 视觉特点                 |
| ------ | ------ | ----------------------- | ------------------------ |
| ACES   | 简单   | 有理多项式曲线          | 标准化，广泛用于电影工业 |
| Filmic | 中等   | S 型曲线（Uncharted 2） | 模拟胶片，高光过渡柔和   |
| AgX    | 复杂   | E-Gamut + Log + 3D LUT  | 最电影化，细节保留最丰富 |

处理流程对比:
ACES: sRGB Linear → 多项式曲线 → sRGB Linear

Filmic: sRGB Linear → S 型曲线函数 → sRGB Linear

AgX: sRGB Linear → E-Gamut → Log2 → 3D LUT → Linear
(4 步变换，最复杂但效果最佳)

---

## 五、色彩空间转换顺序的重要性

为什么色彩空间转换在 Tone Mapping 之前？

1. 算法设计前提: ACES、AgX、Filmic 都是为 sRGB 线性空间设计的
2. 色彩准确性: RAW 传感器色彩空间与 sRGB 不同，必须先转换
3. 避免色偏: 如果在 RAW 空间做 tone mapping，会产生严重色彩失真

正确的顺序:
RAW 空间 → 转到 sRGB 线性空间 → Tone Mapping → Gamma → 输出

错误的顺序会导致:

- 色彩不准确
- 白平衡失效
- 高光/阴影颜色偏移
