package output

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"

	"github.com/weaming/x3f-go/colorspace"
	"github.com/weaming/x3f-go/processor"
)

// JPEGOptions JPEG 输出选项
type JPEGOptions struct {
	Quality      int  // 1-100, 默认 95
	ApplyGamma   bool // 是否应用 gamma 校正
	ToneMapping  bool // 是否应用色调映射
	AutoExposure bool // 是否自动曝光
	Exposure     float64
}

// WriteJPEG 写入 JPEG 文件
func WriteJPEG(img *processor.ProcessedImage, filename string, opts JPEGOptions) error {
	// 创建 Go 标准库的 image.Image
	rgbaImg := image.NewRGBA(image.Rect(0, 0, int(img.Width), int(img.Height)))

	// 转换数据
	for y := 0; y < int(img.Height); y++ {
		for x := 0; x < int(img.Width); x++ {
			idx := (y*int(img.Width) + x) * 3

			rgb := colorspace.Vector3{
				img.Data[idx],
				img.Data[idx+1],
				img.Data[idx+2],
			}

			// 应用额外处理（如果需要）
			if opts.AutoExposure {
				rgb, _ = colorspace.AutoExposure(rgb, 0.18)
			} else if opts.Exposure != 0 {
				rgb = colorspace.SimpleExposure(rgb, opts.Exposure)
			}

			// 应用色调映射
			if opts.ToneMapping {
				rgb = colorspace.ACESToneMapping(rgb)
			}

			// 应用 gamma 校正
			if opts.ApplyGamma {
				rgb = colorspace.ApplySRGBGamma(rgb)
			}

			// 转换为 8-bit
			rgb8 := colorspace.ConvertToUint8(rgb)

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

// ExportJPEG 导出为 JPEG
func ExportJPEG(img *processor.ProcessedImage, filename string, opts JPEGOptions) error {
	if img == nil {
		return fmt.Errorf("图像为空")
	}

	return WriteJPEG(img, filename, opts)
}
