package x3f

import "math"

// ToneMappingMethod 色调映射方法
type ToneMappingMethod string

const (
	ToneMappingACES ToneMappingMethod = "aces"
	ToneMappingAgX  ToneMappingMethod = "agx"
	ToneMappingNone ToneMappingMethod = "none"
)

// 应用 gamma 校正
func ApplyGamma(value, gamma float64) float64 {
	if value <= 0 {
		return 0
	}
	return math.Pow(value, 1.0/gamma)
}

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

// 对 RGB 向量应用 gamma 校正
func ApplyGammaToRGB(rgb Vector3, gamma float64) Vector3 {
	return Vector3{
		ApplyGamma(rgb[0], gamma),
		ApplyGamma(rgb[1], gamma),
		ApplyGamma(rgb[2], gamma),
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

// Clamp 将值限制在 [min, max] 范围内
func Clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// ACES 色调映射（Narkowicz 2015）
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

// AgX 色调映射（完整实现，基于 Troy Sobotka 的 AgX）
func AgXToneMapping(color Vector3) Vector3 {
	// Step 1: 转换到 E-Gamut（从 sRGB）
	// 使用 FilmLight E-Gamut 矩阵（从 OCIO 配置，行主序）
	egamutMatrix := Matrix3x3{
		0.856627153315983, 0.0951212405381588, 0.0482516061458583,
		0.137318972929847, 0.761241990602591, 0.101439036467562,
		0.11189821299995, 0.0767994186031903, 0.811302368396859,
	}
	egamut := egamutMatrix.Apply(color)

	// Step 2: Log2 编码 [-12.47393, 12.5260688117]
	const minLog = -12.47393
	const maxLog = 12.5260688117

	logEncoded := Vector3{}
	for i := 0; i < 3; i++ {
		if egamut[i] <= 0 {
			logEncoded[i] = 0
		} else {
			logVal := math.Log2(egamut[i])
			logEncoded[i] = (logVal - minLog) / (maxLog - minLog)
			logEncoded[i] = Clamp(logEncoded[i], 0, 1)
		}
	}

	// Step 3: 3D LUT 查找（三线性插值）
	lutResult := agxLUT3DLookup(logEncoded)

	// Step 4: LUT 输出已经是显示编码（Rec.1886, gamma 2.4）
	// Rec.1886 (gamma 2.4) 与 sRGB (gamma 2.2) 非常接近
	// 直接使用 LUT 输出，避免 gamma 转换导致的对比度损失
	//
	// 注意：pipeline 会跳过 gamma 校正（因为已经编码）
	// 因此需要配合 LinearOutput=true 或者修改 pipeline 逻辑
	//
	// 【临时修复】：仍然转回线性空间，但使用 gamma 2.2 以匹配后续处理
	// 这样最终 pow(x, 2.2) * pow(x, 1/2.2) = x，保持一致
	result := Vector3{}
	for i := 0; i < 3; i++ {
		// Rec.1886 -> Linear (使用 gamma 2.2 而不是 2.4)
		// 这样与后续的 gamma 2.2 校正相匹配
		result[i] = math.Pow(lutResult[i], 2.2)
		result[i] = Clamp(result[i], 0, 1)
	}

	return result
}

// agxLUT3DLookup 在 3D LUT 中查找值（三线性插值）
func agxLUT3DLookup(color Vector3) Vector3 {
	size := float64(AgX_LUT_SIZE)

	// 将 [0,1] 映射到 LUT 坐标
	r := color[0] * (size - 1)
	g := color[1] * (size - 1)
	b := color[2] * (size - 1)

	// 获取整数和小数部分
	r0 := int(math.Floor(r))
	g0 := int(math.Floor(g))
	b0 := int(math.Floor(b))

	r1 := int(math.Min(float64(r0+1), size-1))
	g1 := int(math.Min(float64(g0+1), size-1))
	b1 := int(math.Min(float64(b0+1), size-1))

	dr := r - float64(r0)
	dg := g - float64(g0)
	db := b - float64(b0)

	// 三线性插值
	// 获取 8 个角点的值
	c000 := agxLUTGetValue(r0, g0, b0)
	c001 := agxLUTGetValue(r0, g0, b1)
	c010 := agxLUTGetValue(r0, g1, b0)
	c011 := agxLUTGetValue(r0, g1, b1)
	c100 := agxLUTGetValue(r1, g0, b0)
	c101 := agxLUTGetValue(r1, g0, b1)
	c110 := agxLUTGetValue(r1, g1, b0)
	c111 := agxLUTGetValue(r1, g1, b1)

	// 插值
	result := Vector3{}
	for i := 0; i < 3; i++ {
		c00 := c000[i]*(1-dr) + c100[i]*dr
		c01 := c001[i]*(1-dr) + c101[i]*dr
		c10 := c010[i]*(1-dr) + c110[i]*dr
		c11 := c011[i]*(1-dr) + c111[i]*dr

		c0 := c00*(1-dg) + c10*dg
		c1 := c01*(1-dg) + c11*dg

		result[i] = c0*(1-db) + c1*db
	}

	return result
}

// agxLUTGetValue 从 LUT 中获取指定坐标的值
func agxLUTGetValue(r, g, b int) Vector3 {
	size := AgX_LUT_SIZE
	index := r + g*size + b*size*size

	if index < 0 || index >= len(AgX_LUT_Data) {
		return Vector3{0, 0, 0}
	}

	rgb := AgX_LUT_Data[index]
	return Vector3{float64(rgb[0]), float64(rgb[1]), float64(rgb[2])}
}

// ApplyToneMapping 应用色调映射
func ApplyToneMapping(color Vector3, method ToneMappingMethod) Vector3 {
	switch method {
	case ToneMappingACES:
		return ACESToneMapping(color)
	case ToneMappingAgX:
		return AgXToneMapping(color)
	case ToneMappingNone:
		return color
	default:
		return AgXToneMapping(color)
	}
}

// 简单的曝光调整
func SimpleExposure(color Vector3, exposure float64) Vector3 {
	scale := math.Pow(2.0, exposure)
	return Vector3{
		color[0] * scale,
		color[1] * scale,
		color[2] * scale,
	}
}

// 将浮点 RGB 转换为 16-bit 整数
func ConvertToUint16(rgb Vector3) [3]uint16 {
	return [3]uint16{
		uint16(math.Min(65535, math.Max(0, rgb[0]*65535+0.5))),
		uint16(math.Min(65535, math.Max(0, rgb[1]*65535+0.5))),
		uint16(math.Min(65535, math.Max(0, rgb[2]*65535+0.5))),
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
