package x3f

import "math"

// ToneMappingMethod 色调映射方法
type ToneMappingMethod string

const (
	ToneMappingACES ToneMappingMethod = "aces"
	ToneMappingAgX  ToneMappingMethod = "agx"
	ToneMappingNone ToneMappingMethod = "none"
)

// sRGB gamma 曲线（精确版本）
func SRGBGamma(linear float64) float64 {
	if linear <= 0.0031308 {
		return 12.92 * linear
	}
	return 1.055*math.Pow(linear, 1.0/2.4) - 0.055
}

// GammaLUT 色调曲线查找表
type GammaLUT struct {
	table []float64
	size  int
}

// NewSRGBLUT 创建sRGB gamma LUT（与C版本x3f_sRGB_LUT一致）
func NewSRGBLUT(size int, maxOut uint16) *GammaLUT {
	lut := &GammaLUT{
		table: make([]float64, size),
		size:  size,
	}

	a := 0.055
	thres := 0.0031308

	for i := 0; i < size; i++ {
		lin := float64(i) / float64(size-1)
		var srgb float64

		if lin <= thres {
			srgb = 12.92 * lin
		} else {
			srgb = (1+a)*math.Pow(lin, 1.0/2.4) - a
		}

		srgb *= float64(maxOut)

		if srgb < 0 {
			lut.table[i] = 0
		} else if srgb > float64(maxOut) {
			lut.table[i] = float64(maxOut)
		} else {
			lut.table[i] = srgb
		}
	}

	return lut
}

// Lookup 在LUT中查找值（与C版本x3f_LUT_lookup一致，使用线性插值）
func (lut *GammaLUT) Lookup(val float64) uint16 {
	index := val * float64(lut.size-1)
	i := int(math.Floor(index))
	frac := index - float64(i)

	if i < 0 {
		return uint16(math.Round(lut.table[0]))
	} else if i >= (lut.size - 1) {
		return uint16(math.Round(lut.table[lut.size-1]))
	} else {
		result := lut.table[i] + frac*(lut.table[i+1]-lut.table[i])
		return uint16(math.Round(result))
	}
}

// 对 RGB 向量应用 sRGB gamma 曲线
func ApplySRGBGamma(rgb Vector3) Vector3 {
	return Vector3{
		SRGBGamma(rgb[0]),
		SRGBGamma(rgb[1]),
		SRGBGamma(rgb[2]),
	}
}

// 将浮点 RGB 转换为 8-bit 整数
func ConvertToUint8(rgb Vector3) [3]uint8 {
	return [3]uint8{
		uint8(math.Min(255, math.Max(0, rgb[0]*255+0.5))),
		uint8(math.Min(255, math.Max(0, rgb[1]*255+0.5))),
		uint8(math.Min(255, math.Max(0, rgb[2]*255+0.5))),
	}
}
