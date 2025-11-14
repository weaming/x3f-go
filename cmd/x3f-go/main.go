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

type Config struct {
	Input           string
	Output          string
	ColorSpace      string
	WhiteBalance    string
	ToneMapping     string
	Verbose         bool
	ShowVersion     bool
	NoCrop          bool
	CompatibleWithC bool
	DumpMeta        bool
	Unprocessed     bool
	Qtop            bool
	Quality         int
	NoDenoise       bool    // 是否禁用降噪（默认启用）
	ExposureValue   float64 // 曝光补偿（EV 值）
}

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

func parseFlags() *Config {
	config := &Config{}

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

func run(config *Config) error {
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

func getColorSpace(name string) x3f.ColorSpace {
	switch strings.ToLower(name) {
	case "none":
		return x3f.ColorSpaceNone
	case "srgb":
		return x3f.ColorSpaceSRGB
	case "adobergb":
		return x3f.ColorSpaceAdobeRGB
	case "prophoto", "prophotorgb":
		return x3f.ColorSpaceProPhotoRGB
	default:
		return x3f.ColorSpaceSRGB
	}
}

func getToneMappingMethod(name string) x3f.ToneMappingMethod {
	switch strings.ToLower(name) {
	case "aces":
		return x3f.ToneMappingACES
	case "agx":
		return x3f.ToneMappingAgX
	case "none":
		return x3f.ToneMappingNone
	default:
		return x3f.ToneMappingAgX
	}
}

func convertToDNG(x3fFile *x3f.File, config *Config, logger *x3f.Logger) error {
	// DNG 只需要预处理，不需要渲染
	wb := config.WhiteBalance
	if wb == "" {
		wb = x3fFile.GetWhiteBalance()
	}

	opts := x3f.ProcessOptions{
		WhiteBalanceType: wb,
		Denoise:          !config.NoDenoise,
		NoCrop:           config.NoCrop,
	}

	preprocessed, err := x3f.ProcessImage(x3fFile, opts, logger)
	if err != nil {
		return err
	}

	// 提取相机信息
	logger.Step("准备 DNG 元数据")
	cameraInfo := x3f.ExtractCameraInfo(x3fFile, wb)

	// 获取 rawSection (需要用于 DNG 输出)
	rawSection := x3fFile.ImageData[len(x3fFile.ImageData)-1]

	dngOpts := output.DNGOptions{
		Camera:  cameraInfo,
		Preproc: preprocessed,
		NoCrop:  config.NoCrop,
	}

	logger.Done(cameraInfo.Model)

	logger.Step("写入 DNG")
	if config.Verbose {
		fmt.Printf("写入 DNG 文件: %s\n", config.Output)
	}

	err = output.ExportRawDNG(x3fFile, rawSection, config.Output, dngOpts)
	if err != nil {
		return err
	}

	logger.Done("完成")

	return nil
}

func convertToTIFF(x3fFile *x3f.File, config *Config, logger *x3f.Logger) error {
	if config.Verbose {
		fmt.Println("转换为 TIFF...")
	}

	// 准备预处理选项
	wb := config.WhiteBalance
	if wb == "" {
		wb = x3fFile.GetWhiteBalance()
	}

	preprocessOpts := x3f.ProcessOptions{
		WhiteBalanceType: wb,
		Denoise:          !config.NoDenoise,
		NoCrop:           config.NoCrop,
	}

	preprocessed, err := x3f.ProcessImage(x3fFile, preprocessOpts, logger)
	if err != nil {
		return err
	}

	// 准备渲染选项
	// TIFF 默认输出 sRGB（和 C 版本一致），应用 gamma 校正
	renderOpts := x3f.RenderOptions{
		ColorSpace:        getColorSpace(config.ColorSpace),
		NoCrop:            config.NoCrop,
		ExposureValue:     config.ExposureValue,
		ToneMappingMethod: x3f.ToneMappingNone, // TIFF 默认不使用色调映射
		LinearOutput:      false,               // 输出 sRGB，应用 gamma 校正
	}

	img, err := x3f.RenderXYZToRGB(x3fFile, preprocessed, renderOpts, logger)
	if err != nil {
		return err
	}

	// 从 X3F 文件中提取 EXIF 元数据
	exif := x3f.ExtractExifInfo(x3fFile)

	tiffOpts := output.TIFFOptions{
		Use16Bit: true,
		Exif:     exif,
	}

	logger.Step("写入 TIFF")
	if config.Verbose {
		fmt.Printf("写入 TIFF 文件: %s\n", config.Output)
	}

	err = output.ExportTIFF(img, config.Output, tiffOpts)
	if err != nil {
		return err
	}

	logger.Done("完成")

	return nil
}

func convertToJPEG(x3fFile *x3f.File, config *Config, logger *x3f.Logger) error {
	if config.Verbose {
		fmt.Println("转换为 JPEG...")
	}

	// 验证 JPEG 质量参数
	quality := config.Quality
	if quality < 1 || quality > 100 {
		return fmt.Errorf("JPEG 质量必须在 1-100 之间，当前值: %d", quality)
	}

	// 准备预处理选项
	wb := config.WhiteBalance
	if wb == "" {
		wb = x3fFile.GetWhiteBalance()
	}

	preprocessOpts := x3f.ProcessOptions{
		WhiteBalanceType: wb,
		Denoise:          !config.NoDenoise,
		NoCrop:           config.NoCrop,
	}

	preprocessed, err := x3f.ProcessImage(x3fFile, preprocessOpts, logger)
	if err != nil {
		return err
	}

	// 准备渲染选项
	// JPEG 在 x3f 包内完成所有 CPU 计算（并发），output 包只做 IO
	renderOpts := x3f.RenderOptions{
		ColorSpace:        getColorSpace(config.ColorSpace),
		NoCrop:            config.NoCrop,
		ExposureValue:     config.ExposureValue,
		ToneMappingMethod: getToneMappingMethod(config.ToneMapping), // 在 x3f 包内应用（并发）
		LinearOutput:      false,                                    // 应用色调映射和 gamma 校正
	}

	img, err := x3f.RenderXYZToRGB(x3fFile, preprocessed, renderOpts, logger)
	if err != nil {
		return err
	}

	jpegOpts := output.JPEGOptions{
		Quality: quality,
		// gamma 和色调映射已在 ProcessImageUnified 中完成
	}

	logger.Step("写入 JPEG")
	if config.Verbose {
		fmt.Printf("写入 JPEG 文件: %s\n", config.Output)
	}

	err = output.ExportJPEG(img, config.Output, jpegOpts)
	if err != nil {
		return err
	}

	logger.Done("完成")

	return nil
}

func convertToPPM(x3fFile *x3f.File, config *Config) error {
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
