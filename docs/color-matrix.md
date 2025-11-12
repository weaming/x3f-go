# DNG 色彩矩阵实现指南

## DNG 色彩转换流程

根据 Adobe DNG 规范，色彩转换的完整流程为：

```
CameraRGB --[CameraCalibration1]--> CalibratedRGB --[inverse(ColorMatrix1)]--> XYZ
```

或者使用 ForwardMatrix:

```
CameraRGB --[CameraCalibration1 × ForwardMatrix1]--> XYZ
```

## Sigma X3F 的色彩模型

Sigma X3F 的 ColorCorrections 矩阵（来自 CAMF 段）已经将 Camera RGB 转换到了**类 sRGB 空间**。

完整的转换链：
```
RawRGB
  --[WhiteBalance]-->
BalancedRGB
  --[ColorCorrections]-->
BMT (≈ sRGB)
  --[sRGB_to_XYZ]-->
XYZ
```

因此，Sigma 的实现使用固定的 sRGB 标准矩阵作为 ColorMatrix1。

## 正确的 Go 实现

### 1. ColorMatrix1 - 固定的 XYZ_to_sRGB 标准矩阵

```go
// GetColorMatrix1ForDNG 返回 XYZ_to_sRGB 标准矩阵
// 这是固定的矩阵，不依赖于相机或白平衡
func GetColorMatrix1ForDNG() []float64 {
    // XYZ_to_sRGB (D65) - 标准矩阵
    return []float64{
        3.2404542, -1.5371385, -0.4985314,
       -0.9692660,  1.8760108,  0.0415560,
        0.0556434, -0.2040259,  1.0572252,
    }
}
```

或者通过 sRGB_to_XYZ 求逆得到：

```go
func GetColorMatrix1ForDNG() []float64 {
    sRGBToXYZ := []float64{
        0.4124564, 0.3575761, 0.1804375,
        0.2126729, 0.7151522, 0.0721750,
        0.0193339, 0.1191920, 0.9503041,
    }
    return Inverse3x3(sRGBToXYZ)
}
```

### 2. ForwardMatrix1 - 相机特定矩阵

```go
// GetForwardMatrix1ForDNG 计算 ForwardMatrix1
// ForwardMatrix1 = D65_to_D50 × bmt_to_xyz
// 其中 bmt_to_xyz = sRGB_to_XYZ × ColorCorrections
func GetForwardMatrix1ForDNG(wb string) []float64 {
    // 从 CAMF 获取 ColorCorrections 矩阵
    ccMatrix := GetColorCorrectionsMatrix(wb)

    // sRGB_to_XYZ 标准矩阵
    sRGBToXYZ := []float64{
        0.4124564, 0.3575761, 0.1804375,
        0.2126729, 0.7151522, 0.0721750,
        0.0193339, 0.1191920, 0.9503041,
    }

    // bmt_to_xyz = sRGB_to_XYZ × ColorCorrections
    bmtToXYZ := multiply3x3(sRGBToXYZ, ccMatrix)

    // D65 到 D50 白点适配矩阵（DNG 要求）
    d65ToD50 := []float64{
        1.0478112, 0.0228866, -0.0501270,
        0.0295424, 0.9904844, -0.0170491,
       -0.0092345, 0.0150436,  0.7521316,
    }

    // ForwardMatrix1 = D65_to_D50 × bmt_to_xyz
    return multiply3x3(d65ToD50, bmtToXYZ)
}
```

### 3. CameraCalibration1 - 白平衡增益矩阵

```go
// GetCameraCalibration1 计算 CameraCalibration1
// 这是一个对角矩阵，包含归一化的白平衡增益倒数
func GetCameraCalibration1(gain [3]float64) []float64 {
    // 归一化：除以最大增益
    maxGain := math.Max(math.Max(gain[0], gain[1]), gain[2])

    // 对角矩阵：1/gain × maxGain
    return []float64{
        1.0 / gain[0] * maxGain, 0, 0,
        0, 1.0 / gain[1] * maxGain, 0,
        0, 0, 1.0 / gain[2] * maxGain,
    }
}
```

### 4. AsShotNeutral - 参考白点

```go
// GetAsShotNeutral 计算 AsShotNeutral
// 这是归一化的白平衡增益倒数（只有 RGB 值，不是矩阵）
func GetAsShotNeutral(gain [3]float64) [3]float64 {
    maxGain := math.Max(math.Max(gain[0], gain[1]), gain[2])

    return [3]float64{
        1.0 / gain[0] * maxGain,
        1.0 / gain[1] * maxGain,
        1.0 / gain[2] * maxGain,
    }
}
```

## 完整的 DNG 写入示例

```go
func ExportToDNG(x3fFile *x3f.File, outputPath string, wb string) error {
    // 获取白平衡增益
    gain := x3fFile.GetWhiteBalanceGain(wb)

    // 计算所有矩阵
    colorMatrix1 := GetColorMatrix1ForDNG()
    forwardMatrix1 := GetForwardMatrix1ForDNG(wb)
    calibration1 := GetCameraCalibration1(gain)
    asShotNeutral := GetAsShotNeutral(gain)

    // 写入 IFD0
    ifd0.AddSRationalArray(TagColorMatrix1,
        floatsToSRationals(colorMatrix1, 10000))
    ifd0.AddSRationalArray(TagForwardMatrix1,
        floatsToSRationals(forwardMatrix1, 10000))
    ifd0.AddSRationalArray(TagCameraCalibration1,
        floatsToSRationals(calibration1, 10000))
    ifd0.AddRationalArray(TagAsShotNeutral,
        floatsToRationals(asShotNeutral[:], 10000))

    // ... 其他 DNG 标签
}
```

## 矩阵乘法和求逆

```go
// multiply3x3 计算两个 3x3 矩阵的乘积
// result = a × b
func multiply3x3(a, b []float64) []float64 {
    result := make([]float64, 9)
    for i := 0; i < 3; i++ {
        for j := 0; j < 3; j++ {
            for k := 0; k < 3; k++ {
                result[i*3+j] += a[i*3+k] * b[k*3+j]
            }
        }
    }
    return result
}

// Inverse3x3 计算 3x3 矩阵的逆矩阵
func Inverse3x3(m []float64) []float64 {
    // 计算行列式
    det := m[0]*(m[4]*m[8]-m[5]*m[7]) -
           m[1]*(m[3]*m[8]-m[5]*m[6]) +
           m[2]*(m[3]*m[7]-m[4]*m[6])

    if math.Abs(det) < 1e-10 {
        panic("matrix is singular")
    }

    // 计算伴随矩阵并除以行列式
    inv := make([]float64, 9)
    inv[0] = (m[4]*m[8] - m[5]*m[7]) / det
    inv[1] = (m[2]*m[7] - m[1]*m[8]) / det
    inv[2] = (m[1]*m[5] - m[2]*m[4]) / det
    inv[3] = (m[5]*m[6] - m[3]*m[8]) / det
    inv[4] = (m[0]*m[8] - m[2]*m[6]) / det
    inv[5] = (m[2]*m[3] - m[0]*m[5]) / det
    inv[6] = (m[3]*m[7] - m[4]*m[6]) / det
    inv[7] = (m[1]*m[6] - m[0]*m[7]) / det
    inv[8] = (m[0]*m[4] - m[1]*m[3]) / det

    return inv
}
```

## 关键要点

1. **ColorMatrix1 是固定的 XYZ_to_sRGB 标准矩阵**
   - 不依赖于相机型号或白平衡设置
   - 所有 Sigma X3F 文件都使用相同的值

2. **ForwardMatrix1 包含相机特定的色彩校正**
   - 基于 CAMF 中的 ColorCorrections 矩阵
   - 不同的白平衡设置会得到不同的矩阵

3. **CameraCalibration1 包含白平衡信息**
   - 对角矩阵，三个对角线元素是归一化的增益倒数
   - 与 AsShotNeutral 的计算方式相同

4. **矩阵顺序很重要**
   - DNG 的色彩转换按照 CameraCalibration1 → inverse(ColorMatrix1) → XYZ 的顺序
   - 或者使用 ForwardMatrix1 替代 inverse(ColorMatrix1)

## 参考

- Adobe DNG Specification 1.4
- Sigma X3F C 代码: `src/x3f_output_dng.c`
- sRGB 标准色彩空间定义 (IEC 61966-2-1)
- libtiff 库文档
