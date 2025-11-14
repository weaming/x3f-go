package x3f

import (
	"fmt"
	"strings"
)

// PreprocessedData 包含预处理后的数据和相关参数
type PreprocessedData struct {
	// 处理后的数据
	Data []uint16
	// 数据尺寸
	Width  uint32
	Height uint32
	// 是否是 Quattro expanded 数据
	IsExpanded bool
	// 是否已转换为 XYZ 色彩空间
	IsXYZ bool
	// Intermediate levels（用于色彩转换）
	IntermediateBias float64
	MaxIntermediate  [3]uint32
	BlackLevel       BlackLevelInfo
}

// PreprocessImage 对图像进行预处理，包括 Quattro expand（如果适用）
// 这是 DNG 和 PPM 输出的共同前置处理流程
func PreprocessImage(file *File, imageSection *ImageSection, profile ProcessOptions, logger *Logger) (*PreprocessedData, error) {
	// ==================== 阶段1: 解码图像 ====================

	// 1.1 解码图像（如果还没解码）
	if imageSection.DecodedData == nil {
		logger.Step("1️⃣  解码 RAW", fmt.Sprintf("%dx%d", imageSection.Columns, imageSection.Rows))
		if err := imageSection.DecodeImage(); err != nil {
			return nil, fmt.Errorf("解码失败: %w", err)
		}
		logger.Done(fmt.Sprintf("%d 像素", len(imageSection.DecodedData)/3))
	}

	// ==================== 阶段2: 准备预处理参数 ====================

	// 2.1 获取白平衡
	wb := profile.WhiteBalanceType
	wbSource := "用户指定"
	if wb == "" {
		wb = file.GetWhiteBalance()
		wbSource = "文件默认"
	}

	// 2.2 计算黑电平
	logger.Step("2️⃣  计算黑电平", fmt.Sprintf("WB=%s (%s)", wb, wbSource))
	blackLevel, err := CalculateBlackLevel(file, imageSection)
	if err != nil {
		return nil, fmt.Errorf("计算黑电平失败: %w", err)
	}
	logger.Done(fmt.Sprintf("R:%.0f G:%.0f B:%.0f",
		blackLevel.Level[0], blackLevel.Level[1], blackLevel.Level[2]))

	// 2.3 获取 intermediate bias
	logger.Step("3️⃣  Intermediate 参数")
	intermediateBias, ok := GetIntermediateBias(file, wb, blackLevel)
	if !ok {
		return nil, fmt.Errorf("获取 intermediate bias 失败")
	}

	// 2.4 获取 max intermediate
	maxIntermediate, ok := GetMaxIntermediate(file, wb, intermediateBias)
	if !ok {
		return nil, fmt.Errorf("获取 max intermediate 失败")
	}
	logger.Done(fmt.Sprintf("Bias:%.0f Max R:%d G:%d B:%d",
		intermediateBias, maxIntermediate[0], maxIntermediate[1], maxIntermediate[2]))

	// ==================== 阶段3: 应用预处理转换 ====================

	// 3.1 应用预处理（黑电平校正、intermediate bias、scale 转换）
	logger.Step("4️⃣  应用预处理转换", "")
	preprocessInfo, err := PreprocessData(file, imageSection, wb)
	if err != nil {
		return nil, fmt.Errorf("预处理失败: %w", err)
	}
	logger.Done(preprocessInfo)

	// 3.2 应用降噪（标准模式：非 Quattro）
	isQuattro := imageSection.QuattroTopData != nil && len(imageSection.QuattroTopData) > 0
	if profile.Denoise && !isQuattro {
		logger.Step("🔇 应用降噪")
		denoiseType := DetectDenoiseType(file)

		area := &Area16{
			Data:      imageSection.DecodedData,
			Rows:      imageSection.DecodedRows,
			Columns:   imageSection.DecodedColumns,
			Channels:  3,
			RowStride: imageSection.DecodedColumns * 3,
		}

		Denoise(area, denoiseType)

		typeName := "STD"
		if denoiseType == DenoiseF20 {
			typeName = "F20"
		} else if denoiseType == DenoiseF23 {
			typeName = "F23"
		}
		logger.Done(fmt.Sprintf("完成 (%s)", typeName))
	}

	// ==================== 阶段4: Quattro Expand（如需要） ====================

	// 4.1 执行 Quattro expand（如果是 Quattro 格式）
	var expandedData []uint16
	var expandedWidth, expandedHeight int
	isExpanded := false

	if isQuattro {
		// 获取降噪类型和配置
		denoiseType := DetectDenoiseType(file)
		config := denoiseConfigs[denoiseType]
		typeName := "STD"
		if denoiseType == DenoiseF20 {
			typeName = "F20"
		} else if denoiseType == DenoiseF23 {
			typeName = "F23"
		}

		// 计算实际使用的 qtop 尺寸（会被裁剪）
		qtopUsedWidth := int(imageSection.DecodedColumns) * 2
		qtopUsedHeight := int(imageSection.DecodedRows) * 2
		if qtopUsedWidth > imageSection.QuattroTopCols {
			qtopUsedWidth = imageSection.QuattroTopCols
		}
		if qtopUsedHeight > imageSection.QuattroTopRows {
			qtopUsedHeight = imageSection.QuattroTopRows
		}

		denoiseInfo := ""
		if profile.Denoise {
			denoiseInfo = fmt.Sprintf(" | 降噪=%s(h=%.0f)", typeName, config.h)
		}

		logger.Step("5️⃣  Quattro Expand",
			fmt.Sprintf("BMT %dx%d + Top %dx%d (原始%dx%d) → 扩展 %dx%d%s",
				imageSection.DecodedColumns, imageSection.DecodedRows,
				qtopUsedWidth, qtopUsedHeight,
				imageSection.QuattroTopCols, imageSection.QuattroTopRows,
				imageSection.DecodedColumns*2, imageSection.DecodedRows*2,
				denoiseInfo))

		// 对 Quattro top 层也应用预处理
		if err := PreprocessQuattroTop(file, imageSection, wb); err != nil {
			return nil, fmt.Errorf("top 层预处理失败: %w", err)
		}

		// 执行 Quattro expand（根据是否需要降噪选择不同的方法）
		if profile.Denoise {
			// 使用带降噪的 expand
			image := &Area16{
				Data:      imageSection.DecodedData,
				Rows:      imageSection.DecodedRows,
				Columns:   imageSection.DecodedColumns,
				Channels:  3,
				RowStride: imageSection.DecodedColumns * 3,
			}

			// 计算 expanded 尺寸
			expandedWidth = int(imageSection.DecodedColumns) * 2
			expandedHeight = int(imageSection.DecodedRows) * 2

			// C 版本会先裁剪 qtop 到 expanded 尺寸
			// rect[0] = 0, rect[1] = 0
			// rect[2] = 2*image.columns - 1
			// rect[3] = 2*image.rows - 1
			qtopWidth := expandedWidth
			qtopHeight := expandedHeight
			if qtopWidth > imageSection.QuattroTopCols {
				qtopWidth = imageSection.QuattroTopCols
			}
			if qtopHeight > imageSection.QuattroTopRows {
				qtopHeight = imageSection.QuattroTopRows
			}

			qtop := &Area16{
				Data:      imageSection.QuattroTopData,
				Rows:      uint32(qtopHeight),
				Columns:   uint32(qtopWidth),
				Channels:  1,
				RowStride: uint32(imageSection.QuattroTopCols), // stride 保持原始值
			}

			expandedData = make([]uint16, expandedWidth*expandedHeight*3)

			expanded := &Area16{
				Data:      expandedData,
				Rows:      uint32(expandedHeight),
				Columns:   uint32(expandedWidth),
				Channels:  3,
				RowStride: uint32(expandedWidth) * 3,
			}

			// 获取 active 区域（从低分辨率 image 中裁剪）
			var active *Area16
			if ax0, ay0, ax1, ay1, ok := file.GetCAMFRectScaled("ActiveImageArea",
				imageSection.DecodedColumns, imageSection.DecodedRows, true); ok {
				active = &Area16{
					Data:      image.Data, // 共享数据
					Rows:      ay1 - ay0 + 1,
					Columns:   ax1 - ax0 + 1,
					Channels:  3,
					RowStride: image.RowStride,
				}
				// 调整数据指针到子区域的起始位置
				offset := int(ay0)*int(image.RowStride) + int(ax0)*int(image.Channels)
				active.Data = image.Data[offset:]
			} else {
				// 如果找不到 ActiveImageArea，使用整个 image
				active = image
			}

			// 获取 active_exp 区域（从高分辨率 expanded 中裁剪）
			// 注意：ActiveImageArea 坐标已经是针对 expanded 尺寸的，不需要缩放
			var activeExp *Area16
			if aex0, aey0, aex1, aey1, ok := file.GetCAMFRectScaled("ActiveImageArea",
				uint32(expandedWidth), uint32(expandedHeight), false); ok {
				activeExp = &Area16{
					Data:      expanded.Data, // 共享数据
					Rows:      aey1 - aey0 + 1,
					Columns:   aex1 - aex0 + 1,
					Channels:  3,
					RowStride: expanded.RowStride,
				}
				// 调整数据指针到子区域的起始位置
				offset := int(aey0)*int(expanded.RowStride) + int(aex0)*int(expanded.Channels)
				activeExp.Data = expanded.Data[offset:]
			} else {
				// 如果找不到，使用整个 expanded
				activeExp = expanded
			}

			ExpandQuattroWithDenoise(image, active, qtop, expanded, activeExp)
		} else {
			// 使用标准 expand（不降噪）
			expandedData = ExpandQuattro(
				imageSection.DecodedData,
				int(imageSection.DecodedColumns),
				int(imageSection.DecodedRows),
				imageSection.QuattroTopData,
				imageSection.QuattroTopCols,
				imageSection.QuattroTopRows,
			)
			expandedWidth = int(imageSection.DecodedColumns) * 2
			expandedHeight = int(imageSection.DecodedRows) * 2
		}

		isExpanded = true
		logger.Done("完成")
	}

	// ==================== 阶段5: 确定输出数据 ====================

	// 5.1 确定输出数据
	var dataToUse []uint16
	var width, height uint32

	if isExpanded && expandedData != nil {
		// 使用 expanded 数据
		dataToUse = expandedData
		width = uint32(expandedWidth)
		height = uint32(expandedHeight)
	} else {
		// 使用原始数据
		dataToUse = imageSection.DecodedData
		width = imageSection.Columns
		height = imageSection.Rows
		if imageSection.DecodedColumns > 0 {
			width = imageSection.DecodedColumns
		}
		if imageSection.DecodedRows > 0 {
			height = imageSection.DecodedRows
		}
	}

	return &PreprocessedData{
		Data:             dataToUse,
		Width:            width,
		Height:           height,
		IsExpanded:       isExpanded,
		IntermediateBias: intermediateBias,
		MaxIntermediate:  maxIntermediate,
		BlackLevel:       blackLevel,
	}, nil
}

// DetectDenoiseType 根据相机型号检测降噪类型
func DetectDenoiseType(file *File) DenoiseType {
	// 检查是否是 Quattro 格式
	if file.Header.Version >= 0x00040000 {
		return DenoiseF23 // Quattro 相机
	}

	// 检查相机型号
	model, ok := file.GetProperty("CAMMODEL")
	if !ok {
		// 从 CAMERAID 获取型号
		model = file.GetCameraModel()
	}

	// F20 相机列表（根据 C 版本的逻辑）
	if strings.Contains(model, "dp2") || strings.Contains(strings.ToLower(model), "dp2") {
		return DenoiseF20
	}

	// 默认使用标准降噪
	return DenoiseSTD
}
