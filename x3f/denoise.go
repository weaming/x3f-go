package x3f

// ===============================================
// NLM (Non-Local Means) 降噪算法
// ===============================================

// DenoiseType 降噪类型
type DenoiseType int

const (
	DenoiseSTD DenoiseType = iota // 标准降噪
	DenoiseF20                    // F20 相机
	DenoiseF23                    // F23 Quattro
)

// denoiseConfig 降噪配置
type denoiseConfig struct {
	h        float64
	bmtToYUV ColorTransformType
	yuvToBMT ColorTransformType
}

var denoiseConfigs = map[DenoiseType]denoiseConfig{
	DenoiseSTD: {h: 100.0, bmtToYUV: BMT_to_YUV_STD, yuvToBMT: YUV_to_BMT_STD},
	DenoiseF20: {h: 70.0, bmtToYUV: BMT_to_YUV_YisT, yuvToBMT: YUV_to_BMT_YisT},
	DenoiseF23: {h: 300.0, bmtToYUV: BMT_to_YUV_Yis4T, yuvToBMT: YUV_to_BMT_Yis4T},
}

// Denoise 主降噪函数
func Denoise(area *Area16, denoiseType DenoiseType) {
	if area.Channels != 3 {
		return
	}

	config, ok := denoiseConfigs[denoiseType]
	if !ok {
		config = denoiseConfigs[DenoiseSTD]
	}

	// 使用 C++ 实现的色彩转换
	ColorTransformOpenCV(area.Data, int(area.Rows), int(area.Columns), int(area.Channels), int(area.RowStride), config.bmtToYUV)

	// 使用 OpenCV 降噪（与 Quattro 降噪保持一致）
	DenoiseWithOpenCV(area.Data, int(area.Rows), int(area.Columns),
		int(area.Channels), int(area.RowStride), config.h)

	// 使用 C++ 实现的色彩转换
	ColorTransformOpenCV(area.Data, int(area.Rows), int(area.Columns), int(area.Channels), int(area.RowStride), config.yuvToBMT)
}

// ===============================================
// Quattro 扩展降噪函数
// ===============================================

// ExpandQuattroWithDenoise 扩展 Quattro 图像并应用降噪
// 对应 C 版本的 x3f_expand_quattro 函数
//
// 参数:
//   - image: 低分辨率 BMT 图像区域
//   - active: 活动区域 (可选，用于第一次降噪)
//   - qtop: 高分辨率 Top 层
//   - expanded: 输出的扩展图像区域
//   - activeExp: 扩展后的活动区域 (可选，用于第二次降噪)
//
// 注意: image、active 和 qtop 会被原地修改
func ExpandQuattroWithDenoise(image, active *Area16, qtop *Area16, expanded, activeExp *Area16) {
	if image.Channels != 3 {
		panic("image must have 3 channels")
	}
	if qtop.Channels != 1 {
		panic("qtop must have 1 channel")
	}

	config := denoiseConfigs[DenoiseF23]

	// 步骤 1: BMT -> YUV (Yis4T 模式) - 使用 C++
	ColorTransformOpenCV(image.Data, int(image.Rows), int(image.Columns), int(image.Channels), int(image.RowStride), config.bmtToYUV)

	// 步骤 2: 如果有 active 区域，先对其降噪
	if active != nil && active.Channels == 3 {
		debug("BEGIN Quattro active area denoising with OpenCV (%dx%d, stride=%d)",
			active.Columns, active.Rows, active.RowStride)

		DenoiseWithOpenCV(active.Data, int(active.Rows), int(active.Columns),
			int(active.Channels), int(active.RowStride), config.h)

		debug("END Quattro active area denoising")
	}

	// 步骤 3: 上采样到扩展尺寸（使用 OpenCV resize，与 C 版本完全一致）
	BicubicUpscaleOpenCV(
		image.Data, int(image.Rows), int(image.Columns), int(image.Channels), int(image.RowStride),
		expanded.Data, int(expanded.Rows), int(expanded.Columns), int(expanded.RowStride),
	)

	// 步骤 4: qtop *= 4 并替换 Y 通道
	for i := 0; i < len(qtop.Data); i++ {
		val := uint32(qtop.Data[i]) * 4
		if val > 65535 {
			val = 65535
		}
		qtop.Data[i] = uint16(val)
	}

	// 替换 Y 通道 (索引 0) - 只替换 qtop 覆盖范围内的像素
	for y := uint32(0); y < expanded.Rows; y++ {
		for x := uint32(0); x < expanded.Columns; x++ {
			// 注意：qtop 可能比 expanded 小，只替换有效范围
			if y < qtop.Rows && x < qtop.Columns {
				qtopIdx := y*qtop.RowStride + x // qtop 是单通道，stride=columns
				expIdx := y*expanded.RowStride + x*expanded.Channels
				expanded.Data[expIdx] = qtop.Data[qtopIdx]
			}
		}
	}

	// 步骤 5: 如果有 activeExp，对扩展后的活动区域再次降噪
	if activeExp != nil && activeExp.Channels == 3 {
		// 使用 Quattro 高分辨率降噪（只执行一次 fastNlMeansDenoising，V 通道 h*2）
		// 对应 C 版本的 x3f_expand_quattro 中的高分辨率降噪逻辑
		debug("BEGIN Quattro full-resolution denoising with OpenCV (%dx%d, stride=%d)",
			activeExp.Columns, activeExp.Rows, activeExp.RowStride)

		DenoiseQuattroHighRes(activeExp.Data, int(activeExp.Rows), int(activeExp.Columns),
			int(activeExp.Channels), int(activeExp.RowStride), config.h)

		debug("END Quattro full-resolution denoising")
	}

	// 步骤 6: YUV -> BMT (Yis4T 模式) - 使用 C++
	ColorTransformOpenCV(expanded.Data, int(expanded.Rows), int(expanded.Columns), int(expanded.Channels), int(expanded.RowStride), config.yuvToBMT)
}
