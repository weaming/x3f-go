package output

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/weaming/x3f-go/colorspace"
	"github.com/weaming/x3f-go/processor"
)

// HEIFOptions HEIF 输出选项
type HEIFOptions struct {
	Quality      int  // 1-100, 默认 90
	ApplyGamma   bool // 是否应用 gamma 校正
	ToneMapping  bool // 是否应用色调映射
	AutoExposure bool // 是否自动曝光
	Exposure     float64
	Use10Bit     bool // 是否使用 10-bit 编码
}

// WriteHEIF 写入 HEIF 文件
func WriteHEIF(img *processor.ProcessedImage, filename string, opts HEIFOptions) error {
	// 检查是否安装了 heif-enc 或其他 HEIF 编码器
	encoder := findHEIFEncoder()
	if encoder == "" {
		return fmt.Errorf("未找到 HEIF 编码器，请安装 libheif-dev 或 heif-enc")
	}

	// 如果使用外部编码器，先创建临时 PNG
	tempPNG := filepath.Join(os.TempDir(), "x3f_temp.png")
	defer os.Remove(tempPNG)

	// 创建 PNG 图像
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

			// 应用处理
			if opts.AutoExposure {
				rgb, _ = colorspace.AutoExposure(rgb, 0.18)
			} else if opts.Exposure != 0 {
				rgb = colorspace.SimpleExposure(rgb, opts.Exposure)
			}

			if opts.ToneMapping {
				rgb = colorspace.ACESToneMapping(rgb)
			}

			if opts.ApplyGamma {
				rgb = colorspace.ApplySRGBGamma(rgb)
			}

			// 转换为 8-bit (或 10-bit 如果支持)
			var r, g, b uint8
			if opts.Use10Bit {
				// 10-bit 需要特殊处理，这里简化为 8-bit
				rgb16 := colorspace.ConvertToUint16(rgb)
				r = uint8(rgb16[0] >> 8)
				g = uint8(rgb16[1] >> 8)
				b = uint8(rgb16[2] >> 8)
			} else {
				rgb8 := colorspace.ConvertToUint8(rgb)
				r, g, b = rgb8[0], rgb8[1], rgb8[2]
			}

			rgbaImg.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}

	// 保存为临时 PNG
	pngFile, err := os.Create(tempPNG)
	if err != nil {
		return err
	}
	err = png.Encode(pngFile, rgbaImg)
	pngFile.Close()
	if err != nil {
		return err
	}

	// 使用外部编码器转换为 HEIF
	quality := opts.Quality
	if quality <= 0 || quality > 100 {
		quality = 90
	}

	cmd := exec.Command(encoder, "-q", fmt.Sprintf("%d", quality), "-o", filename, tempPNG)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("HEIF 编码失败: %w\n输出: %s", err, string(output))
	}

	return nil
}

// findHEIFEncoder 查找可用的 HEIF 编码器
func findHEIFEncoder() string {
	encoders := []string{
		"heif-enc", // libheif
		"avifenc",  // AVIF (HEIF 的变体)
		"magick",   // ImageMagick
		"convert",  // ImageMagick
	}

	for _, enc := range encoders {
		if path, err := exec.LookPath(enc); err == nil {
			return path
		}
	}

	return ""
}

// ExportHEIF 导出为 HEIF
func ExportHEIF(img *processor.ProcessedImage, filename string, opts HEIFOptions) error {
	if img == nil {
		return fmt.Errorf("图像为空")
	}

	return WriteHEIF(img, filename, opts)
}

// ExportHEIFSimple 简化版 HEIF 导出（如果没有外部编码器，返回错误提示）
func ExportHEIFSimple(img *processor.ProcessedImage, filename string) error {
	if img == nil {
		return fmt.Errorf("图像为空")
	}

	encoder := findHEIFEncoder()
	if encoder == "" {
		return fmt.Errorf("HEIF 格式需要外部编码器支持\n" +
			"请安装以下任一工具：\n" +
			"  - libheif (heif-enc)\n" +
			"  - ImageMagick\n" +
			"  - avifenc\n\n" +
			"或使用其他格式：DNG, TIFF, JPEG")
	}

	return WriteHEIF(img, filename, HEIFOptions{
		Quality:     90,
		ApplyGamma:  true,
		ToneMapping: true,
	})
}
