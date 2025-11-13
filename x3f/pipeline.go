package x3f

import (
	"fmt"
	"runtime"
	"sync"
)

// ProcessOptions 图像处理选项
type ProcessOptions struct {
	WhiteBalance      string
	ColorSpace        ColorSpace
	ApplyGamma        bool
	ToneMappingMethod ToneMappingMethod
	LinearOutput      bool
	AutoExposure      bool
	ExposureValue     float64
	NoCrop            bool
	IntermediateOnly  bool // 仅返回 intermediate 数据（用于 DNG）
}

// ProcessedImage 处理后的图像
type ProcessedImage struct {
	Width    uint32
	Height   uint32
	Channels uint32
	Data     []float64 // RGB 浮点数据 [0, 1]
}

// ProcessRAW 处理 RAW 图像数据，执行完整的图像处理流程
//
// 处理流程：
//  1. 解码图像数据（Huffman/未压缩）
//  2. 计算并应用黑电平校正
//  3. 归一化到 [0,1] 范围
//  4. 应用白平衡增益
//  5. 色彩空间转换（RAW → XYZ → RGB）
//  6. 曝光调整（可选）
//  7. 色调映射（可选）
//  8. Gamma 校正（可选）
//
// 使用并发处理以提升性能，根据 CPU 核心数自动分配工作线程
func ProcessRAW(file *File, imageSection *ImageSection, opts ProcessOptions, logger *Logger) (*ProcessedImage, error) {
	// ==================== 阶段1: 准备数据 ====================

	// 1.1 解码图像
	if imageSection.DecodedData == nil {
		logger.Step("解码 RAW", fmt.Sprintf("%dx%d", imageSection.Columns, imageSection.Rows))
		err := imageSection.DecodeImage()
		if err != nil {
			return nil, fmt.Errorf("解码图像失败: %w", err)
		}
		logger.Done(fmt.Sprintf("%d 像素", len(imageSection.DecodedData)/3))
	}

	// 1.2 检查是否需要 Quattro Expand
	isQuattro := imageSection.QuattroTopData != nil && len(imageSection.QuattroTopData) > 0
	wb := opts.WhiteBalance
	if wb == "" {
		wb = file.GetWhiteBalance()
	}

	if isQuattro {
		// 对于 Quattro 格式，使用预处理流程获取完整分辨率的 intermediate 数据
		preprocessed, err := PreprocessImage(file, imageSection, PreprocessOptions{
			WhiteBalance: wb,
			DoExpand:     true,
			Verbose:      false,
		}, logger)
		if err != nil {
			return nil, fmt.Errorf("Quattro 预处理失败: %w", err)
		}

		// 将 intermediate 数据转换为 RGB
		return ProcessIntermediateData(file, preprocessed, opts, logger)
	}

	// 1.3 计算黑电平
	logger.Step("1️⃣  计算黑电平")
	blackLevelInfo, err := CalculateBlackLevel(file, imageSection)
	if err != nil {
		// 使用默认黑电平
		blackLevelInfo = BlackLevelInfo{
			Level: Vector3{0, 0, 0},
			Dev:   Vector3{0, 0, 0},
		}
	}
	blackLevel := Vector3{blackLevelInfo.Level[0], blackLevelInfo.Level[1], blackLevelInfo.Level[2]}
	logger.Done(fmt.Sprintf("R:%.0f G:%.0f B:%.0f", blackLevel[0], blackLevel[1], blackLevel[2]))

	// 1.3 获取最大 RAW 值（用于归一化）
	logger.Step("2️⃣  归一化参数")
	maxRaw, ok := file.GetMaxRAW()
	debug("ProcessRAW: GetMaxRAW returned ok=%v, maxRaw=%v", ok, maxRaw)
	if !ok {
		// TODO: 应该从 ImageDepth 计算,暂时使用 12-bit 的默认值
		maxRaw = [3]uint32{4095, 4095, 4095}
		debug("ProcessRAW: Using default maxRaw=%v", maxRaw)
	}
	logger.Done(fmt.Sprintf("Max R:%d G:%d B:%d", maxRaw[0], maxRaw[1], maxRaw[2]))

	// ==================== 阶段2: 准备色彩转换参数 ====================

	// 2.1 获取白平衡增益
	wb = opts.WhiteBalance
	if wb == "" {
		wb = file.GetWhiteBalance()
	}
	logger.Step("3️⃣  白平衡增益", wb)

	gain, ok := file.GetWhiteBalanceGain(wb)
	if !ok {
		gain = Vector3{1.0, 1.0, 1.0}
	}
	logger.Done(fmt.Sprintf("R:%.3f G:%.3f B:%.3f", gain[0], gain[1], gain[2]))

	// 2.2 获取色彩矩阵 (RAW → XYZ)
	logger.Step("4️⃣  色彩矩阵")
	rawToXYZ, ok := file.GetColorMatrix(wb)
	if !ok {
		// 使用单位矩阵
		rawToXYZ = Identity3x3()
	}

	// 2.3 获取 XYZ → RGB 转换矩阵
	xyzToRGB := Identity3x3()
	var csName string
	if opts.ColorSpace != ColorSpaceNone {
		xyzToRGB = GetXYZToRGBMatrix(opts.ColorSpace)
		switch opts.ColorSpace {
		case ColorSpaceSRGB:
			csName = "sRGB"
		case ColorSpaceAdobeRGB:
			csName = "AdobeRGB"
		case ColorSpaceProPhotoRGB:
			csName = "ProPhotoRGB"
		default:
			csName = "未知"
		}
	} else {
		csName = "线性"
	}
	logger.Done(fmt.Sprintf("RAW→XYZ→%s", csName))

	// ==================== 阶段3: 确定输出尺寸和裁剪区域 ====================

	// 3.1 获取解码尺寸（实际解码出来的图像大小）
	logger.Step("5️⃣  输出尺寸")
	decodedWidth := imageSection.Columns
	decodedHeight := imageSection.Rows
	if imageSection.DecodedColumns > 0 {
		decodedWidth = imageSection.DecodedColumns
	}
	if imageSection.DecodedRows > 0 {
		decodedHeight = imageSection.DecodedRows
	}
	debug("ProcessRAW: imageSection.Columns=%d, Rows=%d", imageSection.Columns, imageSection.Rows)
	debug("ProcessRAW: DecodedColumns=%d, DecodedRows=%d", imageSection.DecodedColumns, imageSection.DecodedRows)
	debug("ProcessRAW: decodedWidth=%d, decodedHeight=%d", decodedWidth, decodedHeight)

	// 3.2 计算目标输出尺寸和裁剪偏移
	var targetWidth, targetHeight uint32
	var cropX, cropY int32

	// 检查是否是 Quattro 格式（需要缩放坐标）
	isQuattro = imageSection.QuattroTopData != nil && len(imageSection.QuattroTopData) > 0

	if opts.NoCrop {
		// 不裁剪，输出完整解码数据
		targetWidth = decodedWidth
		targetHeight = decodedHeight
		cropX = 0
		cropY = 0
		debug("ProcessRAW: NoCrop mode, using full decoded size")
	} else {
		// 使用 ActiveImageArea 裁剪
		x0, y0, x1, y1, ok := file.GetActiveImageArea()
		if ok {
			// 对于 Quattro 格式，ActiveImageArea 是针对 expanded 尺寸的
			// 需要缩放到实际解码尺寸
			if isQuattro {
				x0 = x0 / 2
				y0 = y0 / 2
				x1 = x1 / 2
				y1 = y1 / 2
				debug("ProcessRAW: Quattro detected, scaled ActiveImageArea=[%d, %d, %d, %d]", x0, y0, x1, y1)
			} else {
				debug("ProcessRAW: ActiveImageArea=[%d, %d, %d, %d]", x0, y0, x1, y1)
			}

			// ActiveImageArea 给出的是 [x0, y0, x1, y1]，其中 x1, y1 是 inclusive 的最大坐标
			cropX = int32(x0)
			cropY = int32(y0)
			targetWidth = x1 - x0 + 1
			targetHeight = y1 - y0 + 1
		} else {
			// 没有 ActiveImageArea，使用文件头尺寸
			targetWidth = file.Header.Columns
			targetHeight = file.Header.Rows
			if targetWidth == 0 || targetHeight == 0 {
				targetWidth = decodedWidth
				targetHeight = decodedHeight
			}
			// 居中裁剪
			cropX = int32((decodedWidth - targetWidth) / 2)
			cropY = int32((decodedHeight - targetHeight) / 2)
		}
	}
	debug("ProcessRAW: targetWidth=%d, targetHeight=%d", targetWidth, targetHeight)
	debug("ProcessRAW: cropX=%d, cropY=%d", cropX, cropY)

	cropInfo := ""
	if !opts.NoCrop {
		cropInfo = fmt.Sprintf(" (裁剪自 %dx%d)", decodedWidth, decodedHeight)
	}
	logger.Done(fmt.Sprintf("%dx%d%s", targetWidth, targetHeight, cropInfo))

	// 3.3 创建输出图像结构
	totalPixels := int(targetWidth * targetHeight)
	processed := &ProcessedImage{
		Width:    targetWidth,
		Height:   targetHeight,
		Channels: 3,
		Data:     make([]float64, totalPixels*3),
	}

	rawToXYZMat := rawToXYZ

	// ==================== 阶段4: 数据验证 ====================

	// 4.1 调试输出关键参数
	if len(imageSection.DecodedData) > 0 {
		debug("ProcessRAW: First RAW pixel (0,0) = (%d, %d, %d)",
			imageSection.DecodedData[0], imageSection.DecodedData[1], imageSection.DecodedData[2])
	}
	debug("ProcessRAW: blackLevel = (%.2f, %.2f, %.2f)", blackLevel[0], blackLevel[1], blackLevel[2])
	debug("ProcessRAW: maxRaw = (%d, %d, %d)", maxRaw[0], maxRaw[1], maxRaw[2])
	debug("ProcessRAW: gain = (%.4f, %.4f, %.4f)", gain[0], gain[1], gain[2])

	// 4.2 验证数据大小，防止索引越界
	expectedPixels := int(decodedWidth) * int(decodedHeight)
	actualPixels := len(imageSection.DecodedData) / 3
	if actualPixels < expectedPixels {
		debug("ProcessRAW: Warning - decoded data size mismatch. Expected %d pixels, got %d pixels",
			expectedPixels, actualPixels)
		// 调整解码尺寸以匹配实际数据
		decodedHeight = uint32(actualPixels) / decodedWidth
		debug("ProcessRAW: Adjusted decodedHeight to %d", decodedHeight)

		// 同时需要调整目标尺寸
		if !opts.NoCrop {
			if targetHeight > decodedHeight {
				targetHeight = decodedHeight
			}
		}
	}

	// ==================== 阶段5: 并发像素处理 ====================

	// 5.1 确定并发工作线程数（根据 CPU 核心数）
	numWorkers := runtime.NumCPU()
	if numWorkers > int(targetHeight) {
		numWorkers = int(targetHeight)
	}

	// 构建处理步骤说明
	var steps []string
	steps = append(steps, "黑电平校正")
	steps = append(steps, "归一化[0,1]")
	steps = append(steps, "白平衡")
	steps = append(steps, fmt.Sprintf("色彩转换→%s", csName))
	if opts.AutoExposure {
		steps = append(steps, "自动曝光")
	} else if opts.ExposureValue != 0 {
		steps = append(steps, fmt.Sprintf("曝光±%.1f", opts.ExposureValue))
	}
	if opts.ToneMappingMethod != ToneMappingNone && opts.ToneMappingMethod != "" && !opts.LinearOutput {
		steps = append(steps, fmt.Sprintf("色调映射(%s)", opts.ToneMappingMethod))
	}
	if opts.ApplyGamma && !opts.LinearOutput {
		gamma := GetGamma(opts.ColorSpace)
		steps = append(steps, fmt.Sprintf("Gamma%.1f", gamma))
	}

	stepsInfo := ""
	if len(steps) > 0 {
		stepsInfo = fmt.Sprintf(" | %s", fmt.Sprintf("%v", steps))
	}

	logger.Step("6️⃣  像素处理", fmt.Sprintf("%dx%d (%d 线程)%s", targetWidth, targetHeight, numWorkers, stepsInfo))

	// 5.2 分配每个工作线程处理的行数
	rowsPerWorker := int(targetHeight) / numWorkers
	var wg sync.WaitGroup

	// 5.3 启动并发工作线程
	for workerID := 0; workerID < numWorkers; workerID++ {
		wg.Add(1)

		startRow := workerID * rowsPerWorker
		endRow := startRow + rowsPerWorker
		if workerID == numWorkers-1 {
			endRow = int(targetHeight) // 最后一个线程处理剩余所有行
		}

		// 每个线程处理分配给它的行范围
		go func(startY, endY int) {
			defer wg.Done()

			// 遍历每一行的每个像素
			for outY := uint32(startY); outY < uint32(endY); outY++ {
				for outX := uint32(0); outX < targetWidth; outX++ {
					// === 步骤 A: 读取 RAW 像素 ===

					// 计算在解码图像中的坐标（应用裁剪偏移）
					srcX := int32(outX) + cropX
					srcY := int32(outY) + cropY

					// 边界检查，防止越界
					if srcX < 0 || srcY < 0 || uint32(srcX) >= decodedWidth || uint32(srcY) >= decodedHeight {
						continue
					}

					// 读取 RAW 像素值
					srcIdx := int(srcY)*int(decodedWidth) + int(srcX)
					if srcIdx*3+2 >= len(imageSection.DecodedData) {
						continue
					}

					rawPixel := Vector3{
						float64(imageSection.DecodedData[srcIdx*3]),
						float64(imageSection.DecodedData[srcIdx*3+1]),
						float64(imageSection.DecodedData[srcIdx*3+2]),
					}

					// === 步骤 B: 黑电平校正 ===
					rawPixel[0] -= blackLevel[0]
					rawPixel[1] -= blackLevel[1]
					rawPixel[2] -= blackLevel[2]

					// === 步骤 C: 归一化到 [0, 1] ===
					normalized := Vector3{
						NormalizeToRange(rawPixel[0], float64(maxRaw[0])),
						NormalizeToRange(rawPixel[1], float64(maxRaw[1])),
						NormalizeToRange(rawPixel[2], float64(maxRaw[2])),
					}

					// === 步骤 D: 应用白平衡增益 ===
					normalized = normalized.ComponentMul(gain)

					// === 步骤 E: 色彩空间转换 RAW → XYZ → RGB ===
					xyz := rawToXYZMat.Apply(normalized)
					rgb := xyzToRGB.Apply(xyz)
					rgb = rgb.Clamp(0, 1)

					// === 步骤 F: 可选的后处理 ===

					// F.1 曝光调整
					if opts.AutoExposure {
						rgb, _ = AutoExposure(rgb, 0.18)
					} else if opts.ExposureValue != 0 {
						rgb = SimpleExposure(rgb, opts.ExposureValue)
					}

					// F.2 色调映射
					if opts.ToneMappingMethod != ToneMappingNone && opts.ToneMappingMethod != "" && !opts.LinearOutput {
						rgb = ApplyToneMapping(rgb, opts.ToneMappingMethod)
					}

					// F.3 Gamma 校正
					if opts.ApplyGamma && !opts.LinearOutput {
						gamma := GetGamma(opts.ColorSpace)
						if gamma > 0 {
							rgb = ApplyGammaToRGB(rgb, gamma)
						}
					}

					// === 步骤 G: 存储结果 ===
					outIdx := int(outY)*int(targetWidth) + int(outX)
					processed.Data[outIdx*3] = rgb[0]
					processed.Data[outIdx*3+1] = rgb[1]
					processed.Data[outIdx*3+2] = rgb[2]
				}
			}
		}(startRow, endRow)
	}

	// 5.4 等待所有工作线程完成
	wg.Wait()

	logger.Done("完成")

	return processed, nil
}

// 转换为 16-bit 图像
func (img *ProcessedImage) ToUint16() []uint16 {
	result := make([]uint16, len(img.Data))
	for i, v := range img.Data {
		result[i] = uint16(v*65535 + 0.5)
		if result[i] > 65535 {
			result[i] = 65535
		}
	}
	return result
}

// 转换为 8-bit 图像
func (img *ProcessedImage) ToUint8() []uint8 {
	result := make([]uint8, len(img.Data))
	for i, v := range img.Data {
		result[i] = uint8(v*255 + 0.5)
		if result[i] > 255 {
			result[i] = 255
		}
	}
	return result
}

// ProcessImageUnified 统一的图像处理入口，用于所有输出格式
// 返回预处理后的数据（用于 DNG）或转换为 RGB 的数据（用于 TIFF/JPEG）
func ProcessImageUnified(file *File, opts ProcessOptions, logger *Logger) (*PreprocessedData, *ProcessedImage, error) {
	// 查找 RAW 图像段
	logger.Step("加载图像段")
	var rawSection *ImageSection
	for _, entry := range file.Directory.Entries {
		isImageSection := entry.Type == SECi ||
			entry.Type == IMA2 ||
			entry.Type == IMAG

		if isImageSection {
			if err := file.LoadImageSection(&entry); err != nil {
				continue
			}
		}
	}

	if len(file.ImageData) == 0 {
		return nil, nil, fmt.Errorf("未找到图像数据")
	}

	rawSection = file.ImageData[len(file.ImageData)-1]
	logger.Done("完成")

	// 获取白平衡
	wb := opts.WhiteBalance
	if wb == "" {
		wb = file.GetWhiteBalance()
	}

	// 使用预处理流程（包括黑电平、intermediate、Quattro Expand）
	preprocessed, err := PreprocessImage(file, rawSection, PreprocessOptions{
		WhiteBalance: wb,
		DoExpand:     true, // 始终 expand（对于 Quattro）
		Verbose:      false,
	}, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("预处理失败: %w", err)
	}

	// 如果需要 RGB 输出（TIFF/JPEG），转换 intermediate 数据
	if !opts.IntermediateOnly {
		img, err := ProcessIntermediateData(file, preprocessed, opts, logger)
		if err != nil {
			return nil, nil, err
		}
		return preprocessed, img, nil
	}

	// DNG 只需要 preprocessed 数据
	return preprocessed, nil, nil
}

// ProcessIntermediateData 处理 intermediate 数据并转换为 RGB
// 用于 Quattro 格式的 JPEG/TIFF 输出
func ProcessIntermediateData(file *File, preprocessed *PreprocessedData, opts ProcessOptions, logger *Logger) (*ProcessedImage, error) {
	width := preprocessed.Width
	height := preprocessed.Height
	intermediateBias := preprocessed.IntermediateBias
	maxIntermediate := preprocessed.MaxIntermediate

	logger.Step("1️⃣  归一化参数")
	logger.Done(fmt.Sprintf("Bias:%.0f Max R:%d G:%d B:%d",
		intermediateBias, maxIntermediate[0], maxIntermediate[1], maxIntermediate[2]))

	// 获取白平衡（虽然已经在预处理中应用，但我们需要色彩矩阵）
	wb := opts.WhiteBalance
	if wb == "" {
		wb = file.GetWhiteBalance()
	}

	// 获取色彩矩阵
	logger.Step("2️⃣  色彩矩阵")
	rawToXYZ, ok := file.GetColorMatrix(wb)
	if !ok {
		rawToXYZ = Identity3x3()
	}

	xyzToRGB := Identity3x3()
	var csName string
	if opts.ColorSpace != ColorSpaceNone {
		xyzToRGB = GetXYZToRGBMatrix(opts.ColorSpace)
		switch opts.ColorSpace {
		case ColorSpaceSRGB:
			csName = "sRGB"
		case ColorSpaceAdobeRGB:
			csName = "AdobeRGB"
		case ColorSpaceProPhotoRGB:
			csName = "ProPhotoRGB"
		default:
			csName = "未知"
		}
	} else {
		csName = "线性"
	}
	logger.Done(fmt.Sprintf("RAW→XYZ→%s", csName))

	// 获取输出尺寸和裁剪区域
	logger.Step("3️⃣  输出尺寸")
	var targetWidth, targetHeight uint32
	var cropX, cropY int32

	if opts.NoCrop {
		// 不裁剪，输出完整数据
		targetWidth = width
		targetHeight = height
		cropX = 0
		cropY = 0
		debug("ProcessIntermediateData: NoCrop mode, using full size")
	} else {
		// 使用 ActiveImageArea 裁剪
		// 注意：preprocessed 数据已经是 expanded 尺寸，ActiveImageArea 坐标不需要缩放
		x0, y0, x1, y1, ok := file.GetActiveImageArea()
		if ok {
			debug("ProcessIntermediateData: ActiveImageArea=[%d, %d, %d, %d]", x0, y0, x1, y1)
			cropX = int32(x0)
			cropY = int32(y0)
			targetWidth = x1 - x0 + 1
			targetHeight = y1 - y0 + 1
		} else {
			// 没有 ActiveImageArea，使用文件头尺寸
			targetWidth = file.Header.Columns
			targetHeight = file.Header.Rows
			if targetWidth == 0 || targetHeight == 0 {
				targetWidth = width
				targetHeight = height
			}
			// 居中裁剪
			cropX = int32((width - targetWidth) / 2)
			cropY = int32((height - targetHeight) / 2)
		}
	}
	debug("ProcessIntermediateData: targetWidth=%d, targetHeight=%d, cropX=%d, cropY=%d",
		targetWidth, targetHeight, cropX, cropY)
	logger.Done(fmt.Sprintf("%dx%d", targetWidth, targetHeight))

	// 创建输出图像
	totalPixels := int(targetWidth * targetHeight)
	processed := &ProcessedImage{
		Width:    targetWidth,
		Height:   targetHeight,
		Channels: 3,
		Data:     make([]float64, totalPixels*3),
	}

	// 并发处理像素
	numWorkers := runtime.NumCPU()
	if numWorkers > int(targetHeight) {
		numWorkers = int(targetHeight)
	}

	rowsPerWorker := int(targetHeight) / numWorkers
	var wg sync.WaitGroup

	// 构建处理步骤说明
	var steps []string
	steps = append(steps, fmt.Sprintf("Intermediate归一化→[0,1]"))
	steps = append(steps, fmt.Sprintf("色彩转换→%s", csName))
	if opts.AutoExposure {
		steps = append(steps, "自动曝光")
	} else if opts.ExposureValue != 0 {
		steps = append(steps, fmt.Sprintf("曝光±%.1f", opts.ExposureValue))
	}
	if opts.ToneMappingMethod != ToneMappingNone && opts.ToneMappingMethod != "" && !opts.LinearOutput {
		steps = append(steps, fmt.Sprintf("色调映射(%s)", opts.ToneMappingMethod))
	}
	if opts.ApplyGamma && !opts.LinearOutput {
		gamma := GetGamma(opts.ColorSpace)
		steps = append(steps, fmt.Sprintf("Gamma%.1f", gamma))
	}

	stepsInfo := ""
	if len(steps) > 0 {
		stepsInfo = fmt.Sprintf(" | %s", fmt.Sprintf("%v", steps))
	}

	logger.Step("4️⃣  像素处理", fmt.Sprintf("%dx%d (%d 线程)%s", targetWidth, targetHeight, numWorkers, stepsInfo))

	for workerID := 0; workerID < numWorkers; workerID++ {
		wg.Add(1)

		startRow := workerID * rowsPerWorker
		endRow := startRow + rowsPerWorker
		if workerID == numWorkers-1 {
			endRow = int(targetHeight)
		}

		go func(startY, endY int) {
			defer wg.Done()

			for y := startY; y < endY; y++ {
				for x := 0; x < int(targetWidth); x++ {
					// 源图像坐标（从裁剪区域读取）
					srcX := int(cropX) + x
					srcY := int(cropY) + y
					srcIdx := srcY*int(width) + srcX

					// 输出图像坐标
					outIdx := y*int(targetWidth) + x

					// 读取 intermediate 值
					intermediatePixel := Vector3{
						float64(preprocessed.Data[srcIdx*3]),
						float64(preprocessed.Data[srcIdx*3+1]),
						float64(preprocessed.Data[srcIdx*3+2]),
					}

					// 归一化到 [0, 1]
					normalized := Vector3{
						(intermediatePixel[0] - intermediateBias) / (float64(maxIntermediate[0]) - intermediateBias),
						(intermediatePixel[1] - intermediateBias) / (float64(maxIntermediate[1]) - intermediateBias),
						(intermediatePixel[2] - intermediateBias) / (float64(maxIntermediate[2]) - intermediateBias),
					}

					// 限制范围
					normalized = normalized.Clamp(0, 1)

					// 色彩空间转换
					xyz := rawToXYZ.Apply(normalized)
					rgb := xyzToRGB.Apply(xyz)
					rgb = rgb.Clamp(0, 1)

					// 可选的后处理
					if opts.AutoExposure {
						rgb, _ = AutoExposure(rgb, 0.18)
					} else if opts.ExposureValue != 0 {
						rgb = SimpleExposure(rgb, opts.ExposureValue)
					}

					if opts.ToneMappingMethod != ToneMappingNone && opts.ToneMappingMethod != "" && !opts.LinearOutput {
						rgb = ApplyToneMapping(rgb, opts.ToneMappingMethod)
					}

					if opts.ApplyGamma && !opts.LinearOutput {
						gamma := GetGamma(opts.ColorSpace)
						if gamma > 0 {
							rgb = ApplyGammaToRGB(rgb, gamma)
						}
					}

					// 存储结果
					processed.Data[outIdx*3] = rgb[0]
					processed.Data[outIdx*3+1] = rgb[1]
					processed.Data[outIdx*3+2] = rgb[2]
				}
			}
		}(startRow, endRow)
	}

	wg.Wait()
	logger.Done("完成")

	return processed, nil
}
