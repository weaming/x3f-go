package output

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/weaming/x3f-go/x3f"
)

// HEIFOptions HEIF 输出选项
type HEIFOptions struct {
	Quality  int  // 1-100, 默认 90
	Use10Bit bool // 是否使用 10-bit 编码
}

// 写入 HEIF 文件
func WriteHEIF(img *x3f.ProcessedImage, filename string, opts HEIFOptions) error {
	// 检查是否安装了 heif-enc 或其他 HEIF 编码器
	encoder := findHEIFEncoder()
	if encoder == "" {
		return fmt.Errorf("未找到 HEIF 编码器，请安装 libheif-dev 或 heif-enc")
	}

	// 如果使用外部编码器，先创建临时 PNG
	tempPNG := filepath.Join(os.TempDir(), "x3f_temp.png")
	defer os.Remove(tempPNG)

	// 创建 PNG 图像（不使用并发，只做简单的格式转换）
	// 所有 CPU 密集型计算已经在 ProcessImageUnified 中完成
	rgbaImg := image.NewRGBA(image.Rect(0, 0, int(img.Width), int(img.Height)))

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

// 查找可用的 HEIF 编码器
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

// 导出为 HEIF
func ExportHEIF(img *x3f.ProcessedImage, filename string, opts HEIFOptions) error {
	if img == nil {
		return fmt.Errorf("图像为空")
	}

	return WriteHEIF(img, filename, opts)
}

// 简化版 HEIF 导出（如果没有外部编码器，返回错误提示）
func ExportHEIFSimple(img *x3f.ProcessedImage, filename string) error {
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
		Quality:  90,
		Use10Bit: false,
	})
}
