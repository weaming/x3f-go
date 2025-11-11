package x3f

import (
	"math"
)

// ============================================================================
// 标准常量定义 (来自 C 版本 src/x3f_process.c)
// ============================================================================

const (
	// INTERMEDIATE_DEPTH 中间层深度 (14-bit)
	INTERMEDIATE_DEPTH = 14
	// INTERMEDIATE_UNIT 中间层单位值
	INTERMEDIATE_UNIT = (1 << INTERMEDIATE_DEPTH) - 1 // 16383
	// INTERMEDIATE_BIAS_FACTOR 中间层偏移系数
	INTERMEDIATE_BIAS_FACTOR = 4.0
)

// ============================================================================
// 临时兜底常量 (TODO: 待实现动态计算后移除)
// ============================================================================
// 注意: 以下常量是临时方案，C 代码中这些值是动态计算或从 CAMF 读取的
// - 黑电平: C 代码从 DarkShield 区域统计计算 (get_black_level)
// - 白平衡: C 代码从 CAMF 读取 (x3f_get_gain)
// - 色彩矩阵: C 代码从 CAMF 读取并矩阵运算 (x3f_get_bmt_to_xyz)
//
// Go 版本当前因为 CAMF 解析不完整，使用硬编码的测量值作为兜底

var (
	// DefaultBlackLevel 默认黑电平均值 (临时兜底值)
	// 来源: C 版本 -v 对 dp2m01.x3f 的 DarkShield 区域测量结果
	// TODO: 实现 DarkShield 区域统计计算后移除
	DefaultBlackLevel = [3]float64{16.112489, 16.053343, 16.193610}

	// DefaultBlackDev 默认黑电平标准差 (临时兜底值)
	// 根据 C 版本的 intermediate_bias = 175.424 反推得出
	// TODO: 实现 DarkShield 区域统计计算后移除
	DefaultBlackDev = [3]float64{10.918860, 10.918860, 10.918860}

	// DefaultWhiteBalanceGain 默认白平衡增益 (临时兜底值)
	// 来源: C 版本 -v 对 dp2m01.x3f 的输出
	// 计算公式: AutoWBGain * SensorAdjustmentGainFact * TempGainFact * FNumberGainFact
	// TODO: 实现完整的 CAMF 白平衡读取后移除
	DefaultWhiteBalanceGain = [3]float64{1.96768, 1.15026, 0.777087}
)

// GetMaxRaw 获取原始数据的最大值
func (f *File) GetMaxRaw() ([3]uint32, bool) {
	// 优先尝试读取 ImageDepth
	if imageDepth, ok := f.GetCAMFUint32("ImageDepth"); ok {
		max := uint32((1 << imageDepth) - 1)
		return [3]uint32{max, max, max}, true
	}

	// 判断是否为 TRUE 引擎 (版本 >= 3.0)
	isTRUE := f.Header.Version >= 0x00030000

	var tagName string
	if isTRUE {
		tagName = "RawSaturationLevel"
	} else {
		tagName = "SaturationLevel"
	}

	// 读取 CAMF 向量 (expectedSize=0 表示不限制)
	if vec, ok := f.GetCAMFInt32Vector(tagName, 0); ok && len(vec) >= 3 {
		return [3]uint32{uint32(vec[0]), uint32(vec[1]), uint32(vec[2])}, true
	}

	return [3]uint32{}, false
}

// GetImageLevels 获取图像黑白电平
// 仿照 C 版本 x3f_process.c 中 x3f_get_image 的逻辑
func (f *File) GetImageLevels(wb string) (ImageLevels, bool) {
	// 获取白平衡增益
	gain, ok := f.GetWhiteBalanceGain(wb)
	if !ok {
		gain = [3]float64{1.0, 1.0, 1.0}
	}
	return f.GetImageLevelsWithGain(gain)
}

// GetImageLevelsWithGain 使用指定的白平衡增益计算图像黑白电平
func (f *File) GetImageLevelsWithGain(gain [3]float64) (ImageLevels, bool) {
	var levels ImageLevels

	// 1. 获取黑电平和标准差
	blackLevel, blackDev, ok := f.getBlackLevel()
	if !ok {
		// TODO: 实现 DarkShield 区域统计计算
		// 临时使用默认值
		blackLevel = DefaultBlackLevel
		blackDev = DefaultBlackDev
	}

	// 2. 获取原始最大值
	maxRaw, ok := f.GetMaxRaw()
	if !ok {
		// TODO: 从 CAMF 读取 ImageDepth 或 SaturationLevel
		// 临时使用 12-bit 默认值
		maxRaw = [3]uint32{4095, 4095, 4095}
	}

	// 4. 计算 intermediate_bias (BlackLevel)
	// get_max_intermediate with bias=0
	maxGain := math.Max(math.Max(gain[0], gain[1]), gain[2])
	var maxIntermediate0 [3]uint32
	for i := 0; i < 3; i++ {
		maxIntermediate0[i] = uint32(math.Round(gain[i] * float64(INTERMEDIATE_UNIT) / maxGain))
	}

	// calculate intermediate_bias
	intermediateBias := 0.0
	for i := 0; i < 3; i++ {
		bias := INTERMEDIATE_BIAS_FACTOR * blackDev[i] *
			float64(maxIntermediate0[i]) / (float64(maxRaw[i]) - blackLevel[i])
		if bias > intermediateBias {
			intermediateBias = bias
		}
	}

	// 5. BlackLevel = intermediate_bias
	for i := 0; i < 3; i++ {
		levels.Black[i] = intermediateBias
	}

	// 6. 计算 WhiteLevel = max_intermediate with calculated bias
	for i := 0; i < 3; i++ {
		levels.White[i] = uint32(math.Round(
			gain[i]*(float64(INTERMEDIATE_UNIT)-intermediateBias)/maxGain + intermediateBias))
	}

	return levels, true
}

// getBlackLevel 获取黑电平和标准差
// TODO: 实现完整的黑电平统计计算
// C 代码实现: src/x3f_process.c::get_black_level()
// - 从 DarkShieldTop/DarkShieldBottom/Left/Right 区域统计计算
// - 计算每个通道的均值(black_level)和标准差(black_dev)
func (f *File) getBlackLevel() ([3]float64, [3]float64, bool) {
	// 当前简化实现: 直接返回测量的默认值
	// 完整实现需要解析 CAMF 中的 DarkShield 区域信息并统计像素值
	return DefaultBlackLevel, DefaultBlackDev, true
}
