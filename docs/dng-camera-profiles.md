# DNG Camera Profiles 详解

## 概述

DNG Camera Profile 是嵌入在 DNG 文件中的色彩配置文件，用于定义从 RAW 数据到颜色空间的转换方式。

## C 版本实现分析

### 定义的 6 个 Profiles

在 `x3f_output_dng.c:156-163` 中定义了 6 个 camera profiles：

```c
static const camera_profile_t camera_profiles[] = {
  {"Default", x3f_get_bmt_to_xyz, NULL},                      // Profile 0 (主配置)
  {"Grayscale", get_bmt_to_xyz_noconvert, grayscale_mix_std}, // Profile 1
  {"Grayscale (red filter)", get_bmt_to_xyz_noconvert, grayscale_mix_red},   // Profile 2
  {"Grayscale (blue filter)", get_bmt_to_xyz_noconvert, grayscale_mix_blue}, // Profile 3
  {"Unconverted", get_bmt_to_xyz_noconvert, NULL},            // Profile 4
  {"Linear sRGB", get_bmt_to_xyz_srgb, NULL},                 // Profile 5
};
```

### Profile 存储结构

1. **主 Profile（IFD0）**: 第一个 profile ("Default") 的信息直接写入 IFD0
2. **额外 Profiles（ExtraCameraProfiles）**: 其余 5 个 profiles 以 DCP 格式存储
   - 使用魔法字节 `MMCR` (Big Endian) 标识
   - 通过 `TIFFTAG_EXTRACAMERAPROFILES` (50933) 标签指向偏移量列表
   - 每个额外的 profile 是一个完整的 TIFF-like IFD 结构

### 写入逻辑 (x3f_output_dng.c:227-289)

```c
static x3f_return_t write_camera_profiles(x3f_t *x3f, char *wb,
                                          const camera_profile_t *profiles,
                                          int num, TIFF *tiff)
{
  // 1. 写入第一个 profile 到 IFD0
  if (!write_camera_profile(x3f, wb, &profiles[0], tiff))
    return X3F_ARGUMENT_ERROR;
  TIFFSetField(tiff, TIFFTAG_ASSHOTPROFILENAME, profiles[0].name);

  if (num == 1) return X3F_OK;

  // 2. 写入额外的 profiles
  for (i=1; i < num; i++) {
    // 创建临时 TIFF IFD
    // 写入 profile 数据
    // 添加 "MMCR" 魔法字节
    // 记录偏移量
  }

  // 3. 设置 ExtraCameraProfiles 标签
  TIFFSetField(tiff, TIFFTAG_EXTRACAMERAPROFILES, num-1, profile_offsets);
  return X3F_OK;
}
```

### 默认行为 (x3f_output_dng.c:378-389)

```c
/* Original behavior: use all profiles */
ret = write_camera_profiles(x3f, wb, camera_profiles,
                            sizeof(camera_profiles)/sizeof(camera_profile_t),
                            f_out);
```

**C 版本默认会写入所有 6 个 profiles！**

## Profile 的作用

### 1. 色彩转换方式

每个 profile 定义了不同的色彩转换策略：

- **Default**: 使用相机的 ColorCorrections 矩阵（从 CAMF 读取）
- **Grayscale 系列**: 将彩色图像转换为灰度，支持不同滤镜效果
- **Unconverted**: 不进行色彩转换
- **Linear sRGB**: 使用标准 sRGB 色彩空间矩阵

### 2. ColorMatrix1 和 ForwardMatrix1 的差异

不同的 profile 使用不同的矩阵计算方法：

| Profile | ColorMatrix1 | ForwardMatrix1 |
|---------|-------------|----------------|
| Default | XYZ_to_sRGB (标准矩阵) | D65_to_D50 × bmt_to_xyz (相机特定) |
| Linear sRGB | sRGB_to_XYZ (标准矩阵) | D65_to_D50 × sRGB_to_XYZ |
| Grayscale | 无转换矩阵 | D50_XYZ × grayscale_mix |

### 3. 用户体验

在 Lightroom、Camera Raw 等软件中：
- 用户可以在"相机配置文件"下拉菜单中选择不同的 profile
- 切换 profile 会改变图像的色彩渲染方式
- 不同 profile 适用于不同的后期处理需求

## 对预览效果的影响

### Finder 预览

**测试结果：Camera Profile 不影响 Finder 预览**

实际测试发现：
1. **Finder 优先使用 Preview TIFF**: 如果 DNG 中嵌入了 Preview TIFF，Finder 直接使用它
2. **Camera Profile 被忽略**: Finder 的 Quick Look 不会读取或使用 Camera Profile
3. **预览大小对比**:
   - C 版本（含 Preview TIFF）: qlmanage 生成 490KB PNG
   - Go 版本（无 Preview TIFF）: qlmanage 生成 220KB PNG
   - C 版本的 Preview TIFF 大小: 153951 字节 (~150KB)

**结论：Camera Profile 对 macOS Finder 预览无影响**

### 专业软件预览

在 Lightroom/Camera Raw 中：
- **肯定会影响**: 这些软件会读取所有 profiles
- **用户可选择**: 可以在界面中切换不同的 profile
- **默认使用**: AsShot profile (第一个 profile)

## Go 实现状态

### ✅ 已完成 - 完整的多 Profile 支持

Go 版本已实现与 C 版本完全一致的多 Camera Profile 支持：

#### 实现的 6 个 Profiles：
1. **Default** - 使用相机 CAMF ColorCorrections 矩阵（主 Profile，在 IFD0）
2. **Grayscale** - 标准灰度转换 (1/3, 1/3, 1/3)
3. **Grayscale (red filter)** - 红色滤镜灰度 (2, -1, 0)
4. **Grayscale (blue filter)** - 蓝色滤镜灰度 (0, -1, 2)
5. **Unconverted** - 使用 sRGB 标准矩阵，不转换
6. **Linear sRGB** - 线性 sRGB 色彩空间

#### 核心实现：

**1. `writeCameraProfileIFD()` 函数** (`output/dng.go:107-225`)
- 为单个 profile 生成 Big Endian TIFF IFD 结构
- 包含 tags: Compression, ColorMatrix1, ForwardMatrix1, ProfileName, DefaultBlackRender
- 正确处理 SRATIONAL (type=10) 和 RATIONAL (type=5)
- 生成符合 DCP 格式的完整 TIFF 文件

**2. `writeExtraCameraProfiles()` 函数** (`output/dng.go:228-271`)
- 写入 5 个额外的 profiles（从第 2 个开始）
- 每个 profile 前添加 "MMCR" 魔法字节（Big Endian）
- 2 字节对齐偏移量
- 返回所有 profile 的偏移量数组

**3. ExportRawDNG 集成** (`output/dng.go:636-713`)
- 在 IFD0 中预留 ExtraCameraProfiles 标签
- SubIFD 写入后，写入所有额外 profiles
- 回写偏移量数组到 IFD0

**4. 矩阵计算函数** (`x3f/camf.go:1882-1951`)
- ✓ `GetSRGBToXYZMatrix()` - sRGB 到 XYZ 标准矩阵
- ✓ `GetForwardMatrixWithSRGB()` - 基于 sRGB 的 ForwardMatrix1
- ✓ `GetForwardMatrixGrayscale()` - 灰度模式 ForwardMatrix1

#### 验证结果：

与 C 版本输出对比：
- ✅ 所有 6 个 profiles 都正确生成
- ✅ Profile 名称完全匹配
- ✅ ColorMatrix1 正确（固定 XYZ_to_sRGB 标准矩阵）
- ✅ ForwardMatrix1 数值在浮点精度范围内一致
  - Grayscale profiles: 精确匹配
  - Unconverted/Linear sRGB: 小数点后 6-7 位精度
- ✅ SRATIONAL 符号处理正确（负数正确显示）
- ✅ DCP 格式符合规范（exiftool 可正确读取所有 ProfileIFD）

#### 技术细节：

**DCP 文件格式：**
```
MMCR (4 bytes) + TIFF_DATA (从 offset 4 开始)
```

**TIFF IFD 结构 (Big Endian):**
```
MM 42 [IFD_OFFSET]           // TIFF header (被 MMCR 替换前 4 字节)
[Entry Count]                // 5 entries
[Entry 1: Compression]
[Entry 2: ColorMatrix1]
[Entry 3: ForwardMatrix1]
[Entry 4: ProfileName]
[Entry 5: DefaultBlackRender]
[Next IFD = 0]
[Extra Data...]
```

**关键修复：**
1. SRATIONAL vs RATIONAL - 使用正确的 type (10 vs 5)
2. Signed denominator - SRATIONAL 的分母也用 int32
3. Big Endian - Profile IFDs 使用 Big Endian (与主 DNG 的 Little Endian 不同)

## 技术细节

### ExtraCameraProfiles 标签格式

- **Tag ID**: 50933
- **Type**: LONG (4 字节无符号整数)
- **Count**: N-1 (额外 profile 的数量)
- **Value**: 指向偏移量数组的指针

### DCP 魔法字节

- Big Endian: `MMCR` (0x4D 0x4D 0x43 0x52)
- Little Endian: `IICR` (0x49 0x49 0x52 0x43)

C 代码使用 Big Endian: `fputs("MMCR", tiff_file);`

## Bug 修复记录

### Chroma Blur Radius Tag ID 错误

**问题**: 最初实现时使用了错误的 tag ID

**症状**:
- Go 版本的 exiftool 显示为 `Exif 0xc60f: 0` 而不是 `Chroma Blur Radius: 0`
- C 版本显示正常

**原因**:
- 错误的 tag 定义: `TagChromaBlurRadius = 50703` (0xc60f)
- 正确的 tag 定义: `TagChromaBlurRadius = 50737` (0xc631)

**验证方法**:
```python
# 扫描整个 DNG 文件查找 tag 50703
# 发现 C 和 Go 版本都没有这个 tag

# 检查 C 代码中的定义
deps/src/opencv/3rdparty/libtiff/tiff.h:
#define TIFFTAG_CHROMABLURRADIUS 50737
```

**修复**:
```go
// output/dng.go:34
TagChromaBlurRadius = 50737  // 原来是 50703
```

**结果**:
- ✓ exiftool 现在正确显示 `Chroma Blur Radius: 0`
- ✓ Tag ID 为 0xc631 (50737)
- ✓ 与 C 版本完全一致

## 参考

- C 代码: `/Users/garden/src/x3f/src/x3f_output_dng.c:156-289`
- libtiff 头文件: `/Users/garden/src/x3f/deps/src/opencv/3rdparty/libtiff/tiff.h`
- DNG 规范: DNG v1.2.0.0+ 支持 ExtraCameraProfiles
