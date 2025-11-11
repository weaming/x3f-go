package colorspace

import "math"

// ApplyGamma 应用 gamma 校正
func ApplyGamma(value, gamma float64) float64 {
	if value <= 0 {
		return 0
	}
	return math.Pow(value, 1.0/gamma)
}

// RemoveGamma 移除 gamma 校正（线性化）
func RemoveGamma(value, gamma float64) float64 {
	if value <= 0 {
		return 0
	}
	return math.Pow(value, gamma)
}

// SRGBGamma sRGB gamma 曲线（精确版本）
func SRGBGamma(linear float64) float64 {
	if linear <= 0.0031308 {
		return 12.92 * linear
	}
	return 1.055*math.Pow(linear, 1.0/2.4) - 0.055
}

// SRGBInverseGamma sRGB 逆 gamma 曲线
func SRGBInverseGamma(srgb float64) float64 {
	if srgb <= 0.04045 {
		return srgb / 12.92
	}
	return math.Pow((srgb+0.055)/1.055, 2.4)
}

// ConvertRAWToXYZ 将 RAW 数据转换到 XYZ 色彩空间
func ConvertRAWToXYZ(raw Vector3, rawToXYZ Matrix3x3) Vector3 {
	return rawToXYZ.Apply(raw)
}

// ConvertXYZToRGB 将 XYZ 转换到 RGB 色彩空间
func ConvertXYZToRGB(xyz Vector3, xyzToRGB Matrix3x3) Vector3 {
	return xyzToRGB.Apply(xyz)
}

// ConvertRAWToRGB 将 RAW 数据转换到 RGB 色彩空间
func ConvertRAWToRGB(raw Vector3, rawToXYZ, xyzToRGB Matrix3x3) Vector3 {
	xyz := ConvertRAWToXYZ(raw, rawToXYZ)
	return ConvertXYZToRGB(xyz, xyzToRGB)
}

// ApplyGammaToRGB 对 RGB 向量应用 gamma 校正
func ApplyGammaToRGB(rgb Vector3, gamma float64) Vector3 {
	return Vector3{
		ApplyGamma(rgb[0], gamma),
		ApplyGamma(rgb[1], gamma),
		ApplyGamma(rgb[2], gamma),
	}
}

// ApplySRGBGamma 对 RGB 向量应用 sRGB gamma 曲线
func ApplySRGBGamma(rgb Vector3) Vector3 {
	return Vector3{
		SRGBGamma(rgb[0]),
		SRGBGamma(rgb[1]),
		SRGBGamma(rgb[2]),
	}
}

// NormalizeToRange 将值从 [0, maxVal] 归一化到 [0, 1]
func NormalizeToRange(value, maxVal float64) float64 {
	if maxVal <= 0 {
		return 0
	}
	result := value / maxVal
	if result < 0 {
		return 0
	}
	if result > 1 {
		return 1
	}
	return result
}

// DenormalizeFromRange 将值从 [0, 1] 反归一化到 [0, maxVal]
func DenormalizeFromRange(value, maxVal float64) float64 {
	result := value * maxVal
	if result < 0 {
		return 0
	}
	if result > maxVal {
		return maxVal
	}
	return result
}

// ACESToneMapping ACES 色调映射（Narkowicz 2015）
func ACESToneMapping(color Vector3) Vector3 {
	a := 2.51
	b := 0.03
	c := 2.43
	d := 0.59
	e := 0.14

	result := Vector3{}
	for i := 0; i < 3; i++ {
		x := color[i]
		result[i] = (x * (a*x + b)) / (x*(c*x+d) + e)
		if result[i] < 0 {
			result[i] = 0
		} else if result[i] > 1 {
			result[i] = 1
		}
	}
	return result
}

// ReinhardToneMapping Reinhard 色调映射
func ReinhardToneMapping(color Vector3, whitePoint float64) Vector3 {
	result := Vector3{}
	for i := 0; i < 3; i++ {
		x := color[i]
		numerator := x * (1.0 + x/(whitePoint*whitePoint))
		result[i] = numerator / (1.0 + x)
		if result[i] < 0 {
			result[i] = 0
		} else if result[i] > 1 {
			result[i] = 1
		}
	}
	return result
}

// SimpleExposure 简单的曝光调整
func SimpleExposure(color Vector3, exposure float64) Vector3 {
	scale := math.Pow(2.0, exposure)
	return Vector3{
		color[0] * scale,
		color[1] * scale,
		color[2] * scale,
	}
}

// AutoExposure 自动曝光调整
func AutoExposure(color Vector3, targetBrightness float64) (Vector3, float64) {
	// 计算亮度（使用 Rec. 709 系数）
	brightness := 0.2126*color[0] + 0.7152*color[1] + 0.0722*color[2]

	if brightness <= 0 {
		return color, 1.0
	}

	scale := targetBrightness / brightness
	return Vector3{
		color[0] * scale,
		color[1] * scale,
		color[2] * scale,
	}, scale
}

// ConvertToUint16 将浮点 RGB 转换为 16-bit 整数
func ConvertToUint16(rgb Vector3) [3]uint16 {
	return [3]uint16{
		uint16(math.Min(65535, math.Max(0, rgb[0]*65535+0.5))),
		uint16(math.Min(65535, math.Max(0, rgb[1]*65535+0.5))),
		uint16(math.Min(65535, math.Max(0, rgb[2]*65535+0.5))),
	}
}

// ConvertToUint8 将浮点 RGB 转换为 8-bit 整数
func ConvertToUint8(rgb Vector3) [3]uint8 {
	return [3]uint8{
		uint8(math.Min(255, math.Max(0, rgb[0]*255+0.5))),
		uint8(math.Min(255, math.Max(0, rgb[1]*255+0.5))),
		uint8(math.Min(255, math.Max(0, rgb[2]*255+0.5))),
	}
}

// ConvertFromUint16 将 16-bit 整数转换为浮点 RGB
func ConvertFromUint16(rgb [3]uint16) Vector3 {
	return Vector3{
		float64(rgb[0]) / 65535.0,
		float64(rgb[1]) / 65535.0,
		float64(rgb[2]) / 65535.0,
	}
}

// ConvertFromUint8 将 8-bit 整数转换为浮点 RGB
func ConvertFromUint8(rgb [3]uint8) Vector3 {
	return Vector3{
		float64(rgb[0]) / 255.0,
		float64(rgb[1]) / 255.0,
		float64(rgb[2]) / 255.0,
	}
}
