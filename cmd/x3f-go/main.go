package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/weaming/x3f-go/output"
	"github.com/weaming/x3f-go/x3f"
)

func main() {
	config := parseFlags()

	if config.Input == "" {
		fmt.Fprintln(os.Stderr, "错误: 必须指定输入文件")
		os.Exit(1)
	}

	if config.Output == "" && !config.DumpMeta {
		fmt.Fprintln(os.Stderr, "错误: 必须指定输出文件 (-o) 或使用 -meta")
		os.Exit(1)
	}

	if err := run(config); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() *output.Config {
	config := &output.Config{}

	flag.StringVar(&config.Output, "o", "", "输出文件路径 (必需)")
	flag.StringVar(&config.ColorSpace, "cs", "sRGB",
		"色彩空间: none, sRGB, AdobeRGB, ProPhotoRGB")
	flag.StringVar(&config.WhiteBalance, "wb", "Auto",
		"白平衡: Auto, Sunlight, Shadow, Overcast, Incandescent, Florescent, Flash, Custom, ColorTemp, AutoLSP")
	flag.StringVar(&config.ToneMapping, "tm", "agx", "色调映射: agx, aces, none")
	flag.BoolVar(&config.Verbose, "v", false, "详细输出")
	flag.BoolVar(&config.NoCrop, "no-crop", false, "不裁剪，输出完整解码数据")
	flag.BoolVar(&config.CompatibleWithC, "c", false, "C 兼容模式：生成与 C 版本完全相同的输出（仅 ppm）")
	flag.BoolVar(&config.DumpMeta, "meta", false, "输出元数据到 <输入文件>.meta")
	flag.BoolVar(&config.Unprocessed, "unprocessed", false, "输出未处理的原始 RAW 数据（默认输出预处理后的数据）")
	flag.BoolVar(&config.Qtop, "qtop", false, "输出 Quattro top 层数据（仅用于 Quattro 格式）")
	flag.IntVar(&config.Quality, "quality", 98, "JPEG 质量 (1-100)")
	flag.BoolVar(&config.NoDenoise, "no-denoise", false, "禁用降噪（默认启用降噪，自动检测相机型号）")
	flag.Float64Var(&config.ExposureValue, "ev", 0.0, "曝光补偿 (EV 值, 范围 -3.0 到 +3.0)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "x3f-go version %s\n", x3f.Version)
		fmt.Fprintf(os.Stderr, "\nGo implementation of X3F RAW converter\n\n")
		fmt.Fprintf(os.Stderr, "用法: x3f-go [选项] <输入.x3f>\n\n")
		fmt.Fprintf(os.Stderr, "选项:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n支持的输出格式:\n")
		fmt.Fprintf(os.Stderr, "  .dng   - DNG (Digital Negative, 线性 sRGB)\n")
		fmt.Fprintf(os.Stderr, "  .tiff  - TIFF (线性 sRGB)\n")
		fmt.Fprintf(os.Stderr, "  .jpg   - JPEG (带色调映射和伽马校正)\n")
		fmt.Fprintf(os.Stderr, "  .ppm   - P3 (PPM ASCII, 用于和 C 版本对比测试)\n")
		fmt.Fprintf(os.Stderr, "\n示例:\n")
		fmt.Fprintf(os.Stderr, "  x3f-go -o output.dng input.x3f\n")
		fmt.Fprintf(os.Stderr, "  x3f-go -o output.jpg -wb Sunlight -cs AdobeRGB input.x3f\n")
		fmt.Fprintf(os.Stderr, "  x3f-go -o output.tiff input.x3f\n")
	}

	flag.Parse()

	if flag.NArg() > 0 {
		config.Input = flag.Arg(0)
	}

	return config
}

func run(config *output.Config) error {
	logger := x3f.NewLogger()

	// 步骤 1: 打开文件
	logger.Step("打开文件", filepath.Base(config.Input))
	x3fFile, err := x3f.Open(config.Input)
	if err != nil {
		return fmt.Errorf("无法打开 X3F 文件: %w", err)
	}
	defer x3fFile.Close()
	logger.Done(fmt.Sprintf("版本=%.1f", float64(x3fFile.Header.Version)/65536.0))

	// 步骤 2: 加载元数据
	logger.Step("加载元数据")
	if err := x3fFile.LoadSection(x3f.SECp); err != nil && config.Verbose {
		logger.Warn("属性段加载失败")
	}
	if err := x3fFile.LoadSection(x3f.SECc); err != nil && config.Verbose {
		logger.Warn("CAMF 段加载失败")
	}
	logger.Done("完成")

	// 如果指定了 -meta,输出元数据并退出
	if config.DumpMeta {
		return dumpMetadata(x3fFile, config)
	}

	// 确定输出格式
	outputExt := strings.ToLower(filepath.Ext(config.Output))
	if config.Verbose {
		logger.Info("输出: %s, 色彩空间: %s, 白平衡: %s",
			outputExt, config.ColorSpace, config.WhiteBalance)
	}

	// 检查 -c 参数只能用于 ppm 格式
	if config.CompatibleWithC && outputExt != ".ppm" {
		return fmt.Errorf("-c 参数只支持 PPM 格式，当前格式: %s", outputExt)
	}

	// 调度到对应的转换函数
	var convertErr error
	switch outputExt {
	case ".dng":
		convertErr = convertToDNG(x3fFile, config, logger)
	case ".tiff", ".tif":
		convertErr = convertToTIFF(x3fFile, config, logger)
	case ".jpg", ".jpeg":
		convertErr = convertToJPEG(x3fFile, config, logger)
	case ".ppm":
		convertErr = convertToPPM(x3fFile, config)
	default:
		return fmt.Errorf("不支持的输出格式: %s", outputExt)
	}

	if convertErr == nil {
		logger.Total()
	}
	return convertErr
}

func convertToDNG(x3fFile *x3f.File, config *output.Config, logger *x3f.Logger) error {
	// 提取相机信息
	logger.Step("准备 DNG 元数据")
	cameraInfo := x3f.ExtractCameraInfo(x3fFile, config.WhiteBalance)
	logger.Done(cameraInfo.Model)

	logger.Step("写入 DNG")
	if config.Verbose {
		fmt.Printf("写入 DNG 文件: %s\n", config.Output)
	}

	commonData, err := output.ProcessAll(x3fFile, *config, logger)
	if err != nil {
		return err
	}

	err = output.ExportRawDNG(commonData, x3fFile, config.Output, cameraInfo, logger)
	if err != nil {
		return err
	}

	logger.Done("完成")
	return nil
}

func convertToTIFF(x3fFile *x3f.File, config *output.Config, logger *x3f.Logger) error {
	if config.Verbose {
		fmt.Println("转换为 TIFF...")
	}

	commonData, err := output.ProcessAll(x3fFile, *config, logger)
	if err != nil {
		return err
	}

	return output.ExportTIFF(commonData, x3fFile, *config, config.Output, logger)
}

func convertToJPEG(x3fFile *x3f.File, config *output.Config, logger *x3f.Logger) error {
	if config.Verbose {
		fmt.Println("转换为 JPEG...")
	}

	commonData, err := output.ProcessAll(x3fFile, *config, logger)
	if err != nil {
		return err
	}

	return output.ExportJPEG(commonData, x3fFile, *config, config.Output, logger)
}

func convertToPPM(x3fFile *x3f.File, config *output.Config) error {
	if config.Qtop {
		if config.Verbose {
			fmt.Println("转换为 PPM（Quattro top 层数据）...")
		}
	} else if config.Unprocessed {
		if config.Verbose {
			fmt.Println("转换为 PPM（未处理的 RAW 数据）...")
		}
	} else {
		if config.Verbose {
			fmt.Println("转换为 PPM（预处理后的数据）...")
		}
	}

	// 加载 RAW 图像段
	logger := x3f.NewLogger()
	rawSection, err := x3fFile.LoadRawImageSection(logger)
	if err != nil {
		return err
	}

	if config.Verbose {
		fmt.Printf("写入 PPM 文件: %s\n", config.Output)
	}

	if config.Qtop {
		// 导出 Quattro top 层数据
		return output.ExportQtopPPM(rawSection, x3fFile, config.Output, config.NoCrop)
	} else if config.Unprocessed {
		// 导出未处理的 RAW 数据
		return output.ExportRawPPM(rawSection, x3fFile, config.Output, config.NoCrop)
	} else {
		// 导出预处理后的数据
		wb := config.WhiteBalance
		if wb == "" {
			wb = x3fFile.GetWhiteBalance()
		}
		return output.ExportPreprocessedPPM(rawSection, x3fFile, config.Output, config.NoCrop, wb)
	}
}
