package x3f

import (
	"fmt"
	"runtime"
	"sync"
)

// RenderOptions 渲染选项
// 用于 RenderXYZToRGB，包含渲染阶段的参数
type RenderOptions struct {
	ColorSpace        ColorSpace
	NoCrop            bool
	ExposureValue     float64           // 曝光补偿 (EV)
	ToneMappingMethod ToneMappingMethod // 色调映射方法
	LinearOutput      bool              // 是否输出线性数据（不应用 gamma 和色调映射）
}

// ProcessedImage 处理后的图像
type ProcessedImage struct {
	Width    uint32
	Height   uint32
	Channels uint32
	Data     []float64 // RGB 浮点数据 [0, 1]
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

// ProcessImage 统一的图像处理入口，用于所有输出格式
// 返回预处理数据（包含 XYZ 数据）
// 注意：preprocessed.Data 在转换后包含 XYZ 数据（uint16）
func ProcessImage(file *File, opts ProcessOptions, logger *Logger) (*PreprocessedData, error) {
	// 查找 RAW 图像段
	rawSection, err := file.LoadRawImageSection(logger)
	if err != nil {
		return nil, err
	}

	// 确定白平衡类型
	wb := opts.WhiteBalanceType
	if wb == "" {
		wb = file.GetWhiteBalance()
	}

	// 使用预处理流程（包括黑电平、intermediate、Quattro Expand）
	preprocessed, err := PreprocessImage(file, rawSection, opts, logger)
	if err != nil {
		return nil, fmt.Errorf("预处理失败: %w", err)
	}

	// 应用白平衡并转换到 XYZ（所有格式都需要）
	// 注意：这会 in-place 修改 preprocessed.Data，从 intermediate 转换为 XYZ
	err = applyWhiteBalance(file, preprocessed, wb, logger)
	if err != nil {
		return nil, fmt.Errorf("XYZ 转换失败: %w", err)
	}

	return preprocessed, nil
}

// LoadRawImageSection 查找并加载 RAW 图像段
func (f *File) LoadRawImageSection(logger *Logger) (*ImageSection, error) {
	logger.Step("加载图像段")

	for _, entry := range f.Directory.Entries {
		isImageSection := entry.Type == SECi ||
			entry.Type == IMA2 ||
			entry.Type == IMAG

		if isImageSection {
			if err := f.LoadImageSection(&entry); err != nil {
				continue
			}
		}
	}

	if len(f.ImageData) == 0 {
		return nil, fmt.Errorf("未找到图像数据")
	}

	rawSection := f.ImageData[len(f.ImageData)-1]
	logger.Done("完成")

	return rawSection, nil
}

// applyWhiteBalance 应用白平衡并转换到 XYZ 色彩空间
// 这是所有格式共同需要的处理步骤，直接在 preprocessed.Data 上操作（in-place）
// XYZ 是设备无关的中间色彩空间，可以转换到任何目标色彩空间（sRGB/AdobeRGB/ProPhotoRGB 等）
func applyWhiteBalance(file *File, preprocessed *PreprocessedData, wb string, logger *Logger) error {
	if preprocessed.IsXYZ {
		return nil // 已经转换过了
	}

	width := preprocessed.Width
	height := preprocessed.Height
	intermediateBias := preprocessed.IntermediateBias
	maxIntermediate := preprocessed.MaxIntermediate

	logger.Step("5️⃣  白平衡和色彩转换")

	// 获取 RAW → XYZ 矩阵（包含白平衡）
	rawToXYZ, ok := file.GetRawToXYZ(wb)
	if !ok {
		return fmt.Errorf("无法获取 RAW → XYZ 矩阵")
	}

	logger.Done(fmt.Sprintf("WB=%s → XYZ", wb))

	// 并发处理像素
	numWorkers := runtime.NumCPU()
	if numWorkers > int(height) {
		numWorkers = int(height)
	}

	rowsPerWorker := int(height) / numWorkers
	var wg sync.WaitGroup

	logger.Step("6️⃣  转换像素", fmt.Sprintf("%dx%d (%d 线程)", width, height, numWorkers))

	maxOut := 65535.0

	for workerID := 0; workerID < numWorkers; workerID++ {
		wg.Add(1)

		startRow := workerID * rowsPerWorker
		endRow := startRow + rowsPerWorker
		if workerID == numWorkers-1 {
			endRow = int(height)
		}

		go func(startY, endY int) {
			defer wg.Done()

			for y := startY; y < endY; y++ {
				for x := 0; x < int(width); x++ {
					idx := y*int(width) + x
					offset := idx * 3

					// 读取 intermediate 值
					r := float64(preprocessed.Data[offset])
					g := float64(preprocessed.Data[offset+1])
					b := float64(preprocessed.Data[offset+2])

					// 归一化到 [0, 1]
					input := Vector3{
						(r - intermediateBias) / (float64(maxIntermediate[0]) - intermediateBias),
						(g - intermediateBias) / (float64(maxIntermediate[1]) - intermediateBias),
						(b - intermediateBias) / (float64(maxIntermediate[2]) - intermediateBias),
					}

					// 限制范围
					input = input.Clamp(0, 1)

					// 应用 RAW → XYZ 转换（包含白平衡）
					xyz := rawToXYZ.Apply(input)

					// 转换回 16-bit 并存储
					for c := 0; c < 3; c++ {
						val := xyz[c] * maxOut
						if val < 0 {
							val = 0
						} else if val > maxOut {
							val = maxOut
						}
						preprocessed.Data[offset+c] = uint16(val)
					}
				}
			}
		}(startRow, endRow)
	}

	wg.Wait()
	logger.Done("完成")

	// 标记数据已转换为 XYZ
	preprocessed.IsXYZ = true

	return nil
}

// RenderXYZToRGB 从 XYZ 数据转换到 RGB 色彩空间
// 用于 JPEG/TIFF 输出
// 输入的 preprocessed.Data 应该已经是 XYZ 格式（uint16）
func RenderXYZToRGB(file *File, preprocessed *PreprocessedData, opts RenderOptions, logger *Logger) (*ProcessedImage, error) {
	if !preprocessed.IsXYZ {
		return nil, fmt.Errorf("数据尚未转换为 XYZ 格式")
	}

	width := preprocessed.Width
	height := preprocessed.Height

	logger.Step("7️⃣  色彩空间转换")

	// 获取 XYZ → RGB 矩阵
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
	logger.Done(fmt.Sprintf("XYZ → %s", csName))

	// 获取输出尺寸和裁剪区域
	logger.Step("8️⃣  输出尺寸")
	var targetWidth, targetHeight uint32
	var cropX, cropY int32

	if opts.NoCrop {
		// 不裁剪，输出完整数据
		targetWidth = width
		targetHeight = height
		cropX = 0
		cropY = 0
	} else {
		// 使用 ActiveImageArea 裁剪
		x0, y0, x1, y1, ok := file.GetActiveImageArea()
		if ok {
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
	steps = append(steps, fmt.Sprintf("XYZ→%s", csName))
	if opts.ExposureValue != 0 {
		steps = append(steps, fmt.Sprintf("曝光±%.1f", opts.ExposureValue))
	}
	if !opts.LinearOutput {
		if opts.ToneMappingMethod != ToneMappingNone && opts.ToneMappingMethod != "" {
			steps = append(steps, fmt.Sprintf("色调映射(%s)", opts.ToneMappingMethod))
		}
		gamma := GetGamma(opts.ColorSpace)
		steps = append(steps, fmt.Sprintf("Gamma%.1f", gamma))
	}

	stepsInfo := ""
	if len(steps) > 0 {
		stepsInfo = fmt.Sprintf(" | %s", fmt.Sprintf("%v", steps))
	}

	logger.Step("9️⃣  像素处理", fmt.Sprintf("%dx%d (%d 线程)%s", targetWidth, targetHeight, numWorkers, stepsInfo))

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

					// 读取 XYZ 值
					xyz := Vector3{
						float64(preprocessed.Data[srcIdx*3]) / 65535.0,
						float64(preprocessed.Data[srcIdx*3+1]) / 65535.0,
						float64(preprocessed.Data[srcIdx*3+2]) / 65535.0,
					}

					// 转换到 RGB
					rgb := xyzToRGB.Apply(xyz)
					rgb = rgb.Clamp(0, 1)

					// 可选的曝光补偿
					if opts.ExposureValue != 0 {
						rgb = SimpleExposure(rgb, opts.ExposureValue)
					}

					if !opts.LinearOutput {
						// 应用色调映射
						if opts.ToneMappingMethod != ToneMappingNone && opts.ToneMappingMethod != "" {
							rgb = ApplyToneMapping(rgb, opts.ToneMappingMethod)
						}
						// 应用 gamma 校正
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
