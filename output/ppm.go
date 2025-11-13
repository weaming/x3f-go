package output

import (
	"fmt"
	"os"

	"github.com/weaming/x3f-go/x3f"
)

// 导出为 PPM 格式（用于调试）
func ExportPPM(img *x3f.ProcessedImage, outputPath string) error {
	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// 写入 PPM 头部
	fmt.Fprintf(f, "P3\n%d %d\n65535\n", img.Width, img.Height)

	// 写入像素数据（float64 [0,1] -> uint16 [0,65535]）
	for i := 0; i < len(img.Data); i += 3 {
		r := uint16(img.Data[i] * 65535.0)
		g := uint16(img.Data[i+1] * 65535.0)
		b := uint16(img.Data[i+2] * 65535.0)
		fmt.Fprintf(f, "%d %d %d\n", r, g, b)
	}

	return nil
}

// ExportRawPPM 导出未处理的 RAW 数据为 PPM 格式（用于调试）
// noCrop=true 时输出完整未裁剪的数据
// 对于 Quattro 格式，会执行 expand（匹配 C 版本 -unprocessed 行为），但不做预处理
func ExportRawPPM(imageSection *x3f.ImageSection, file *x3f.File, outputPath string, noCrop bool) error {
	// 解码图像（如果还没解码）
	if imageSection.DecodedData == nil {
		if err := imageSection.DecodeImage(); err != nil {
			return fmt.Errorf("解码失败: %w", err)
		}
	}

	// ===============================================
	// -unprocessed 模式：输出原始解码数据
	// ===============================================
	// C 版本的 -unprocessed 模式：
	// 1. 不执行 expand_quattro
	// 2. 不执行 preprocess_data
	// 3. 直接输出解码后的原始数据
	// ===============================================

	// 直接使用解码数据，不进行任何处理
	dataToUse := imageSection.DecodedData
	decodedWidth := imageSection.Columns
	decodedHeight := imageSection.Rows
	if imageSection.DecodedColumns > 0 {
		decodedWidth = imageSection.DecodedColumns
	}
	if imageSection.DecodedRows > 0 {
		decodedHeight = imageSection.DecodedRows
	}

	var targetWidth, targetHeight uint32
	var cropX, cropY int32

	if noCrop {
		// 不裁剪，输出完整解码数据
		targetWidth = decodedWidth
		targetHeight = decodedHeight
		cropX = 0
		cropY = 0
	} else {
		// 应用裁剪：使用 ActiveImageArea，并根据实际图像尺寸进行缩放
		// rescale=true 表示将坐标从 KeepImageArea 的分辨率缩放到实际图像分辨率
		x0, y0, x1, y1, ok := file.GetCAMFRectScaled("ActiveImageArea", decodedWidth, decodedHeight, true)
		if ok {
			// GetCAMFRectScaled 返回的坐标已经是相对于图像原点的
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

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// 写入 PPM 头部
	fmt.Fprintf(f, "P3\n%d %d\n65535\n", targetWidth, targetHeight)

	// 写入像素数据（直接从 dataToUse 读取，不经过预处理）
	for outY := uint32(0); outY < targetHeight; outY++ {
		for outX := uint32(0); outX < targetWidth; outX++ {
			srcX := int32(outX) + cropX
			srcY := int32(outY) + cropY
			srcIdx := int(srcY)*int(decodedWidth) + int(srcX)

			r := dataToUse[srcIdx*3]
			g := dataToUse[srcIdx*3+1]
			b := dataToUse[srcIdx*3+2]

			fmt.Fprintf(f, "%d %d %d \n", r, g, b)
		}
	}

	return nil
}

// ExportPreprocessedPPM 导出预处理后并进行色彩转换的数据为 PPM
// 用于对比 C 版本的默认 PPM 输出（预处理 + expand + 色彩转换 + gamma校正）
func ExportPreprocessedPPM(imageSection *x3f.ImageSection, file *x3f.File, outputPath string, noCrop bool, wb string) error {
	// 使用共享的预处理函数
	// PPM 导出不需要详细日志，使用空 logger
	logger := x3f.NewLogger()
	preprocessed, err := x3f.PreprocessImage(file, imageSection, x3f.PreprocessOptions{
		WhiteBalance: wb,
		DoExpand:     true, // PPM 输出需要 expand
		Verbose:      false,
	}, logger)
	if err != nil {
		return err
	}

	// 提取预处理后的数据
	dataToUse := preprocessed.Data
	decodedWidth := preprocessed.Width
	decodedHeight := preprocessed.Height
	isExpanded := preprocessed.IsExpanded
	intermediateBias := preprocessed.IntermediateBias
	maxIntermediate := preprocessed.MaxIntermediate

	var targetWidth, targetHeight uint32
	var cropX, cropY int32

	if noCrop {
		targetWidth = decodedWidth
		targetHeight = decodedHeight
		cropX = 0
		cropY = 0
	} else {
		// 对于expanded数据，ActiveImageArea坐标已经是针对expanded尺寸的，使用rescale=0
		// 对于未expanded数据，需要rescale=1将坐标从KeepImageArea缩放到实际尺寸
		rescale := !isExpanded
		x0, y0, x1, y1, ok := file.GetCAMFRectScaled("ActiveImageArea", decodedWidth, decodedHeight, rescale)
		if ok {
			cropX = int32(x0)
			cropY = int32(y0)
			targetWidth = x1 - x0 + 1
			targetHeight = y1 - y0 + 1
		} else {
			targetWidth = file.Header.Columns
			targetHeight = file.Header.Rows
			if targetWidth == 0 || targetHeight == 0 {
				targetWidth = decodedWidth
				targetHeight = decodedHeight
			}
			cropX = int32((decodedWidth - targetWidth) / 2)
			cropY = int32((decodedHeight - targetHeight) / 2)
		}
	}

	// 获取色彩转换矩阵 (BMT -> XYZ -> sRGB)
	rawToXYZ, ok := file.GetColorMatrix(wb)
	if !ok {
		rawToXYZ = x3f.Identity3x3()
	}
	xyzToRGB := x3f.GetXYZToRGBMatrix(x3f.ColorSpaceSRGB)
	rawToRGB := xyzToRGB.Multiply(rawToXYZ)

	// 应用 ISO scaling (capture_iso / sensor_iso)
	isoScaling := 1.0
	if sensorISO, ok1 := file.GetCAMFFloat("SensorISO"); ok1 {
		if captureISO, ok2 := file.GetCAMFFloat("CaptureISO"); ok2 {
			isoScaling = captureISO / sensorISO
		}
	}

	// 应用 ISO scaling 到转换矩阵
	convMatrix := rawToRGB.Scale(isoScaling)

	// 计算 intermediate levels 的 black 和 white
	ilevelsBlack := [3]float64{intermediateBias, intermediateBias, intermediateBias}
	ilevelsWhite := [3]float64{
		float64(maxIntermediate[0]),
		float64(maxIntermediate[1]),
		float64(maxIntermediate[2]),
	}

	// 创建 sRGB gamma LUT（与C版本一致：1024个条目）
	const LUTSIZE = 1024
	lut := x3f.NewSRGBLUT(LUTSIZE, 65535)

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// 写入 PPM 头部
	fmt.Fprintf(f, "P3\n%d %d\n65535\n", targetWidth, targetHeight)

	// 写入色彩转换后的像素数据
	for outY := uint32(0); outY < targetHeight; outY++ {
		for outX := uint32(0); outX < targetWidth; outX++ {
			srcX := int32(outX) + cropX
			srcY := int32(outY) + cropY
			srcIdx := int(srcY)*int(decodedWidth) + int(srcX)

			// 读取 intermediate 值
			r := float64(dataToUse[srcIdx*3])
			g := float64(dataToUse[srcIdx*3+1])
			b := float64(dataToUse[srcIdx*3+2])

			// 归一化到 [0, 1]
			input := x3f.Vector3{
				(r - ilevelsBlack[0]) / (ilevelsWhite[0] - ilevelsBlack[0]),
				(g - ilevelsBlack[1]) / (ilevelsWhite[1] - ilevelsBlack[1]),
				(b - ilevelsBlack[2]) / (ilevelsWhite[2] - ilevelsBlack[2]),
			}

			// 应用色彩矩阵转换 (BMT -> XYZ -> sRGB)
			output := convMatrix.Apply(input)

			// 使用LUT应用 sRGB gamma 校正（与C版本一致）
			outR := lut.Lookup(output[0])
			outG := lut.Lookup(output[1])
			outB := lut.Lookup(output[2])

			fmt.Fprintf(f, "%d %d %d \n", outR, outG, outB)
		}
	}

	return nil
}

// ExportQtopPPM 导出 Quattro top 层数据为 PPM 格式
func ExportQtopPPM(imageSection *x3f.ImageSection, file *x3f.File, outputPath string, noCrop bool) error {
	// 解码图像（如果还没解码）
	if imageSection.DecodedData == nil {
		if err := imageSection.DecodeImage(); err != nil {
			return fmt.Errorf("解码失败: %w", err)
		}
	}

	// 检查是否有 Quattro top 层数据
	if imageSection.QuattroTopData == nil {
		return fmt.Errorf("此文件不是 Quattro 格式或缺少 top 层数据")
	}

	topWidth := imageSection.QuattroTopCols
	topHeight := imageSection.QuattroTopRows

	var targetWidth, targetHeight int
	var cropX, cropY int

	if noCrop {
		// 不裁剪，输出完整 top 层数据
		targetWidth = topWidth
		targetHeight = topHeight
		cropX = 0
		cropY = 0
	} else {
		// 应用裁剪：使用 ActiveImageArea
		x0, y0, x1, y1, ok := file.GetActiveImageArea()
		if ok {
			// ActiveImageArea 是针对完整图像的坐标，需要转换到 top 层坐标
			// top 层的尺寸通常是 bottom/middle 层的 2 倍
			cropX = int(x0)
			cropY = int(y0)
			targetWidth = int(x1 - x0 + 1)
			targetHeight = int(y1 - y0 + 1)
		} else {
			// 没有 ActiveImageArea，使用完整 top 层
			targetWidth = topWidth
			targetHeight = topHeight
			cropX = 0
			cropY = 0
		}
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// 写入 PPM 头部（单通道，输出为灰度图，即 R=G=B）
	fmt.Fprintf(f, "P3\n%d %d\n65535\n", targetWidth, targetHeight)

	// 写入 top 层像素数据（单通道，复制为 RGB）
	for outY := 0; outY < targetHeight; outY++ {
		for outX := 0; outX < targetWidth; outX++ {
			srcX := outX + cropX
			srcY := outY + cropY

			if srcX >= topWidth || srcY >= topHeight {
				// 超出范围，输出黑色
				fmt.Fprintf(f, "0 0 0 \n")
				continue
			}

			srcIdx := srcY*topWidth + srcX
			val := imageSection.QuattroTopData[srcIdx]

			// top 层是单通道数据，输出为灰度图（R=G=B）
			fmt.Fprintf(f, "%d %d %d \n", val, val, val)
		}
	}

	return nil
}
