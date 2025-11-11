## 最科学的 X3F 处理流程

X3F（Sigma Foveon X3）是**三层传感器** - 和普通 Bayer 完全不同的架构。

---

## X3F 的特殊性

### 普通相机（Bayer）
```
单层传感器 + 彩色滤镜阵列：
R G R G
G B G B  ← 需要去马赛克（插值）
R G R G
```

### Sigma Foveon X3
```
三层硅片：
顶层 ━━━ 蓝光 (B)
中层 ━━━ 绿光 (G)  ← 每个位置都有完整 RGB
底层 ━━━ 红光 (R)
```

**优势**：
- 无需去马赛克（真正的 RGB 数据）
- 色彩准确（无插值伪影）
- 细节锐利（无 AA 滤镜）

**劣势**：
- 高 ISO 噪声大
- 动态范围小
- 色彩不线性（需要复杂校正）

---

## 科学处理流程

### 完整管线

```
📷 X3F 文件
    ↓
【1】读取三层 RAW 数据
    ↓
【2】传感器校正（Dark Frame / Hot Pixel）
    ↓
【3】线性化（Foveon 特有的非线性响应）
    ↓
【4】色彩校正矩阵（X3F → CIE XYZ）
    ↓
【5】白平衡（在线性 RGB 空间）
    ↓
【6】去噪（保留边缘）
    ↓
【7】转到工作色彩空间（ACES / ProPhoto RGB）
    ↓
【8】曝光 / 对比度调整
    ↓
【9】Tone Mapping（动态范围压缩）
    ↓
【10】锐化（可选）
    ↓
【11】输出色彩空间（sRGB / P3）
```

---

## 详细步骤

### 【1】读取 X3F 数据

**推荐工具**：LibRaw（支持 X3F）

```python
import rawpy
import numpy as np

# 读取 X3F
with rawpy.imread("SDIM0001.X3F") as raw:
    # Foveon 数据已经是 RGB，不需要 demosaic
    rgb = raw.postprocess(
        use_camera_wb=False,  # 不用相机白平衡（手动控制）
        use_auto_wb=False,
        no_auto_bright=True,  # 不自动亮度
        output_bps=16,        # 16-bit 输出
        gamma=(1, 1),         # 线性（无 gamma）
        output_color=rawpy.ColorSpace.raw  # 原始色彩空间
    )
```

**注意**：X3F 的"RAW"已经是每像素 RGB，不是 Bayer 的单通道。

---

### 【2】传感器校正

#### Dark Frame 减法（暗电流）
```python
# 拍摄暗场参考（盖上镜头盖，相同 ISO/曝光）
dark_frame = load_dark_frame(iso=200, exposure=1/125)

# 减去暗电流
corrected = rgb.astype(np.float32) - dark_frame
corrected = np.maximum(corrected, 0)  # 防止负值
```

#### 热像素修复
```python
from scipy.ndimage import median_filter

# 检测异常高值
threshold = np.percentile(corrected, 99.9)
hot_pixels = corrected > threshold

# 中值滤波修复
for channel in range(3):
    mask = hot_pixels[:, :, channel]
    if mask.any():
        corrected[:, :, channel][mask] = median_filter(
            corrected[:, :, channel], size=3
        )[mask]
```

---

### 【3】线性化（关键！）

Foveon 传感器的**响应曲线不是完全线性的**，尤其在暗部和高光。

```python
# Sigma 的线性化曲线（近似）
def foveon_linearization(raw_value, sensor_model="sd_quattro"):
    # 不同型号有不同曲线
    if sensor_model == "sd_quattro":
        # 基于 LibRaw 的实现
        # 低值：接近线性
        # 高值：轻微压缩
        normalized = raw_value / 65535.0
        
        # 三段式线性化
        linear = np.where(
            normalized < 0.01,
            normalized * 10.0,  # 暗部提升
            np.where(
                normalized < 0.9,
                normalized,  # 中间线性
                0.9 + (normalized - 0.9) * 2.0  # 高光扩展
            )
        )
        return linear
    
# 应用到每个通道
for c in range(3):
    corrected[:, :, c] = foveon_linearization(corrected[:, :, c])
```

**原因**：Foveon 的光电转换特性和 CMOS 不同，硅的吸收深度影响响应。

---

### 【4】色彩校正矩阵（CCM）

X3F 的 RGB **不等于** 标准 RGB，需要转换到 CIE XYZ。

```python
# Sigma 相机的色彩校正矩阵（示例，实际需要校准）
# 从 X3F RGB → XYZ
CCM_X3F_to_XYZ = np.array([
    [ 0.4124,  0.3576,  0.1805],  # R 通道对 XYZ 的贡献
    [ 0.2126,  0.7152,  0.0722],  # G 通道
    [ 0.0193,  0.1192,  0.9505]   # B 通道
])

# 注意：真实矩阵需要用色卡校准
# 不同镜头、不同批次传感器都可能不同

# 应用 CCM
h, w, c = corrected.shape
rgb_flat = corrected.reshape(-1, 3)
xyz = rgb_flat @ CCM_X3F_to_XYZ.T
xyz = xyz.reshape(h, w, 3)
```

**获取准确 CCM**：
1. 拍摄 ColorChecker 色卡
2. 用软件（如 DCamProf）计算矩阵
3. 或使用 LibRaw 内置的矩阵

---

### 【5】白平衡

在**线性 XYZ 或 RGB** 空间做白平衡。

```python
# 读取相机记录的白平衡系数
with rawpy.imread("SDIM0001.X3F") as raw:
    wb_coeffs = raw.camera_whitebalance  # [R_gain, G_gain, B_gain]
    # 例如：[2.1, 1.0, 1.6]

# 归一化（G 通道为 1.0）
wb_coeffs = wb_coeffs / wb_coeffs[1]

# 应用白平衡
balanced = xyz.copy()
balanced[:, :, 0] *= wb_coeffs[0]  # R 通道
balanced[:, :, 1] *= wb_coeffs[1]  # G 通道
balanced[:, :, 2] *= wb_coeffs[2]  # B 通道
```

**或者手动白平衡**：
```python
# 用户点击灰色区域
gray_region = xyz[100:150, 200:250, :]
gray_avg = np.mean(gray_region, axis=(0, 1))

# 计算增益（目标：灰色的 RGB 相等）
target_gray = np.mean(gray_avg)
wb_coeffs = target_gray / gray_avg

# 应用
balanced = xyz * wb_coeffs
```

---

### 【6】去噪（重要！）

Foveon 高 ISO 噪声**很大**，需要强力去噪但保留边缘。

#### 方法 1：Non-Local Means
```python
from skimage.restoration import denoise_nl_means, estimate_sigma

# 估计噪声水平
sigma_est = estimate_sigma(balanced, channel_axis=2)

# NLM 去噪（保留纹理）
denoised = denoise_nl_means(
    balanced,
    h=1.15 * sigma_est,  # 去噪强度
    patch_size=5,
    patch_distance=7,
    channel_axis=2,
    fast_mode=True
)
```

#### 方法 2：Bilateral Filter（更快）
```python
from skimage.restoration import denoise_bilateral

denoised = denoise_bilateral(
    balanced,
    sigma_color=0.05,   # 色彩相似度
    sigma_spatial=15,   # 空间距离
    channel_axis=2
)
```

#### 方法 3：深度学习（最强）
```python
# 用预训练模型（如 DnCNN, FFDNet）
import torch
model = load_pretrained_denoiser()
denoised = model(torch.from_numpy(balanced)).numpy()
```

**ISO > 800 建议用深度学习去噪**。

---

### 【7】转到工作色彩空间

转换到广色域空间（ACES / ProPhoto RGB）。

```python
# XYZ → ACES AP1 (ACEScg)
XYZ_to_ACES = np.array([
    [ 1.0498, -0.4959, -0.0000],
    [-0.4959,  1.3733,  0.0982],
    [ 0.0000,  0.0000,  0.9911]
])

aces = denoised @ XYZ_to_ACES.T
aces = np.maximum(aces, 0)  # 防止负值
```

**或者用 OpenColorIO**：
```python
import PyOpenColorIO as OCIO

config = OCIO.Config.CreateFromFile("aces_1.2_config.ocio")
processor = config.getProcessor("Linear - Rec.709", "ACES - ACEScg")

# 应用变换
aces = processor.applyRGB(denoised)
```

---

### 【8】曝光 / 对比度调整

```python
# 曝光补偿（stops）
exposure_stops = 0.5  # +0.5 EV
aces_exposed = aces * (2 ** exposure_stops)

# 对比度（在 log 空间）
import numpy as np

def adjust_contrast(image, contrast=1.2):
    # 转到 log 空间
    log_image = np.log2(image + 0.0001)
    
    # 调整对比度（围绕中灰 0.18）
    middle_gray = np.log2(0.18)
    adjusted = (log_image - middle_gray) * contrast + middle_gray
    
    # 转回线性
    return 2 ** adjusted

aces_contrasted = adjust_contrast(aces_exposed, contrast=1.15)
```

---

### 【9】Tone Mapping

Foveon 动态范围小（~10-11 stops），但仍需压缩。

#### ACES RRT（推荐）
```python
def aces_rrt_simplified(rgb):
    """简化版 ACES RRT tone mapping"""
    a = 2.51
    b = 0.03
    c = 2.43
    d = 0.59
    e = 0.14
    
    return (rgb * (a * rgb + b)) / (rgb * (c * rgb + d) + e)

tonemapped = aces_rrt_simplified(aces_contrasted)
tonemapped = np.clip(tonemapped, 0, 1)
```

#### Reinhard（备选）
```python
def reinhard_extended(rgb, max_white=1.5):
    """扩展 Reinhard"""
    numerator = rgb * (1.0 + (rgb / (max_white ** 2)))
    denominator = 1.0 + rgb
    return numerator / denominator

tonemapped = reinhard_extended(aces_contrasted, max_white=2.0)
```

---

### 【10】锐化（可选）

Foveon 本身很锐利，但可以微调。

```python
from scipy.ndimage import gaussian_filter

def unsharp_mask(image, sigma=1.0, strength=0.5):
    blurred = gaussian_filter(image, sigma=sigma, channel_axis=2)
    sharpened = image + strength * (image - blurred)
    return np.clip(sharpened, 0, 1)

sharpened = unsharp_mask(tonemapped, sigma=0.8, strength=0.3)
```

**注意**：Foveon 容易过锐，strength 建议 < 0.5。

---

### 【11】输出色彩空间

```python
# ACES → sRGB
ACES_to_sRGB = np.array([
    [ 2.5216, -1.1347, -0.3869],
    [-0.2765,  1.3722, -0.0956],
    [-0.0153, -0.1525,  1.1678]
])

srgb_linear = tonemapped @ ACES_to_sRGB.T
srgb_linear = np.clip(srgb_linear, 0, 1)

# 应用 sRGB gamma（2.2）
def apply_srgb_gamma(linear):
    return np.where(
        linear <= 0.0031308,
        linear * 12.92,
        1.055 * (linear ** (1/2.4)) - 0.055
    )

srgb = apply_srgb_gamma(srgb_linear)

# 转为 8-bit
output = (srgb * 255).astype(np.uint8)
```

---

## 完整代码示例

```python
import rawpy
import numpy as np
from skimage.restoration import denoise_bilateral
import PyOpenColorIO as OCIO

def process_x3f(filename):
    # 1. 读取
    with rawpy.imread(filename) as raw:
        rgb = raw.postprocess(
            use_camera_wb=False,
            gamma=(1, 1),
            output_bps=16,
            output_color=rawpy.ColorSpace.raw
        )
        wb = raw.camera_whitebalance
    
    # 2. 归一化到 [0, 1]
    rgb = rgb.astype(np.float32) / 65535.0
    
    # 3. 白平衡
    wb = wb / wb[1]
    rgb *= wb
    
    # 4. 去噪
    rgb = denoise_bilateral(rgb, sigma_color=0.05, sigma_spatial=15, channel_axis=2)
    
    # 5. 色彩校正（LibRaw 已做，这里用 OCIO）
    config = OCIO.Config.CreateFromFile("aces_1.2_config.ocio")
    processor = config.getProcessor("Linear - Rec.709", "ACES - ACEScg")
    aces = processor.applyRGB(rgb)
    
    # 6. 曝光
    aces *= 1.2  # +0.26 stops
    
    # 7. Tone mapping (ACES RRT)
    def aces_tonemap(x):
        a, b, c, d, e = 2.51, 0.03, 2.43, 0.59, 0.14
        return (x * (a * x + b)) / (x * (c * x + d) + e)
    
    tonemapped = aces_tonemap(np.maximum(aces, 0))
    
    # 8. 转 sRGB
    processor_out = config.getProcessor("ACES - ACEScg", "Output - sRGB")
    srgb = processor_out.applyRGB(tonemapped)
    
    # 9. sRGB gamma
    srgb = np.where(srgb <= 0.0031308, srgb * 12.92, 1.055 * (srgb ** (1/2.4)) - 0.055)
    srgb = np.clip(srgb, 0, 1)
    
    return (srgb * 255).astype(np.uint8)

# 使用
output = process_x3f("SDIM0001.X3F")
```

---

## 特殊注意事项

### ① 色彩校准
Foveon 的色彩**不如 Bayer 准确**，必须：
- 拍摄 ColorChecker
- 用 DCamProf 生成 DCP 配置文件
- 或使用社区分享的配置

### ② 高 ISO 策略
```
ISO ≤ 400  → 正常流程
ISO 800    → 强去噪 + 轻微锐化
ISO ≥ 1600 → 深度学习去噪 + 放弃部分细节
```

### ③ 动态范围有限
- 优先保护高光（ETTR 拍摄）
- 暗部提亮会有噪声，谨慎
- 避免极端对比场景

### ④ Moiré 问题
虽然无 Bayer 滤镜，但 Foveon 仍可能在细密纹理产生摩尔纹：
```python
# 轻微模糊消除
if detect_moire(image):
    rgb = gaussian_filter(rgb, sigma=0.3, channel_axis=2)
```

---

## 推荐工具链

### 完整方案
```
LibRaw → OpenColorIO → 自定义 Python
```

### 快速方案
```
Sigma Photo Pro (官方，但闭源)
RawTherapee (开源，支持 X3F)
```

### 最科学（研究用）
```
1. LibRaw 提取原始三层数据
2. 用 ColorChecker 校准 CCM
3. ACES 工作流处理
4. 保存为 OpenEXR (32-bit float)
```

---

## 总结

**X3F 处理的科学性在于**：

1. **尊重三层架构** - 不当成 Bayer 处理
2. **线性化** - Foveon 响应曲线非标准
3. **精确色彩校正** - CCM 比 Bayer 更关键
4. **强力去噪** - 高 ISO 是弱项
5. **保守 tone mapping** - 动态范围有限

**核心**：X3F 是"真彩色"传感器，但需要更多后期补偿硬件劣势（噪声、动态范围）。