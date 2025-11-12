package output

import (
	"fmt"
	"os"

	"github.com/weaming/x3f-go/processor"
	"github.com/weaming/x3f-go/x3f"
)

// ExportPPM 导出为 PPM 格式（用于调试）
func ExportPPM(img *processor.ProcessedImage, outputPath string) error {
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
func ExportRawPPM(imageSection *x3f.ImageSection, file *x3f.File, outputPath string, noCrop bool) error {
	// 解码图像（如果还没解码）
	if imageSection.DecodedData == nil {
		if err := imageSection.DecodeImage(); err != nil {
			return fmt.Errorf("解码失败: %w", err)
		}
	}

	// 确定输出尺寸
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
		// 应用裁剪：使用 ActiveImageArea
		x0, y0, x1, y1, ok := file.GetActiveImageArea()
		fmt.Printf("ExportRawPPM: ActiveImageArea: ok=%v, [%d, %d, %d, %d]\n", ok, x0, y0, x1, y1)
		if ok {
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

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// 写入 PPM 头部
	fmt.Fprintf(f, "P3\n%d %d\n65535\n", targetWidth, targetHeight)

	// 调试输出
	fmt.Printf("ExportRawPPM: decodedWidth=%d, decodedHeight=%d\n", decodedWidth, decodedHeight)
	fmt.Printf("ExportRawPPM: targetWidth=%d, targetHeight=%d\n", targetWidth, targetHeight)
	fmt.Printf("ExportRawPPM: cropX=%d, cropY=%d\n", cropX, cropY)

	// 写入像素数据（直接从 DecodedData 读取，不经过任何处理）
	for outY := uint32(0); outY < targetHeight; outY++ {
		for outX := uint32(0); outX < targetWidth; outX++ {
			srcX := int32(outX) + cropX
			srcY := int32(outY) + cropY
			srcIdx := int(srcY)*int(decodedWidth) + int(srcX)

			r := imageSection.DecodedData[srcIdx*3]
			g := imageSection.DecodedData[srcIdx*3+1]
			b := imageSection.DecodedData[srcIdx*3+2]

			// 调试：输出第一个像素
			if outY == 0 && outX == 0 {
				fmt.Printf("ExportRawPPM: 第一个像素 [%d,%d] -> srcIdx=%d, RGB=(%d, %d, %d)\n",
					outX, outY, srcIdx, r, g, b)
			}

			fmt.Fprintf(f, "%d %d %d \n", r, g, b)
		}
	}

	return nil
}

// ExportPreprocessedPPM 导出预处理后但未进行色彩转换的数据为 PPM
// 用于对比 C 版本的默认 PPM 输出（也是预处理后的中间数据）
func ExportPreprocessedPPM(imageSection *x3f.ImageSection, file *x3f.File, outputPath string, noCrop bool, wb string) error {
	// 解码图像（如果还没解码）
	if imageSection.DecodedData == nil {
		if err := imageSection.DecodeImage(); err != nil {
			return fmt.Errorf("解码失败: %w", err)
		}
	}

	// 应用预处理
	if err := processor.PreprocessData(file, imageSection, wb); err != nil {
		return fmt.Errorf("预处理失败: %w", err)
	}

	// 确定输出尺寸
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
		targetWidth = decodedWidth
		targetHeight = decodedHeight
		cropX = 0
		cropY = 0
	} else {
		x0, y0, x1, y1, ok := file.GetActiveImageArea()
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

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// 写入 PPM 头部
	fmt.Fprintf(f, "P3\n%d %d\n65535\n", targetWidth, targetHeight)

	// 写入预处理后的像素数据
	for outY := uint32(0); outY < targetHeight; outY++ {
		for outX := uint32(0); outX < targetWidth; outX++ {
			srcX := int32(outX) + cropX
			srcY := int32(outY) + cropY
			srcIdx := int(srcY)*int(decodedWidth) + int(srcX)

			r := imageSection.DecodedData[srcIdx*3]
			g := imageSection.DecodedData[srcIdx*3+1]
			b := imageSection.DecodedData[srcIdx*3+2]

			fmt.Fprintf(f, "%d %d %d \n", r, g, b)
		}
	}

	return nil
}
