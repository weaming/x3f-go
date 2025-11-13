package output

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"

	"github.com/weaming/x3f-go/x3f"
)

// JPEGOptions JPEG 输出选项
type JPEGOptions struct {
	Quality int // 1-100, 默认 95
}

// 写入 JPEG 文件
func WriteJPEG(img *x3f.ProcessedImage, filename string, opts JPEGOptions) error {
	// 创建 Go 标准库的 image.Image
	rgbaImg := image.NewRGBA(image.Rect(0, 0, int(img.Width), int(img.Height)))

	// 转换数据到 RGBA 格式（不使用并发，只是简单的格式转换）
	// 所有 CPU 密集型计算（gamma、色调映射等）已经在 ProcessImageUnified 中完成
	for y := 0; y < int(img.Height); y++ {
		for x := 0; x < int(img.Width); x++ {
			idx := (y*int(img.Width) + x) * 3

			// 直接转换为 8-bit（所有处理已在 ProcessImageUnified 中完成）
			rgb8 := x3f.ConvertToUint8(x3f.Vector3{
				img.Data[idx],
				img.Data[idx+1],
				img.Data[idx+2],
			})

			rgbaImg.SetRGBA(x, y, color.RGBA{
				R: rgb8[0],
				G: rgb8[1],
				B: rgb8[2],
				A: 255,
			})
		}
	}

	// 创建文件
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// 设置质量
	quality := opts.Quality
	if quality <= 0 || quality > 100 {
		quality = 95
	}

	// 编码 JPEG
	return jpeg.Encode(file, rgbaImg, &jpeg.Options{Quality: quality})
}

// 导出为 JPEG
func ExportJPEG(img *x3f.ProcessedImage, filename string, opts JPEGOptions) error {
	if img == nil {
		return fmt.Errorf("图像为空")
	}

	return WriteJPEG(img, filename, opts)
}
