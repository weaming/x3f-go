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
	JPEGQuality     int
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
	flag.StringVar(&config.ToneMapping, "tm", "agx",
		"色调映射: agx, aces, none")
	flag.BoolVar(&config.Verbose, "v", false,
		"详细输出")
	flag.BoolVar(&config.NoCrop, "no-crop", false,
		"不裁剪，输出完整解码数据")
	flag.BoolVar(&config.CompatibleWithC, "c", false,
		"C 兼容模式：生成与 C 版本完全相同的输出（完整图像+Active Area）")
	flag.BoolVar(&config.DumpMeta, "meta", false,
		"输出元数据到 <输入文件>.meta")
	flag.BoolVar(&config.Unprocessed, "unprocessed", false,
		"输出未处理的原始 RAW 数据（默认输出预处理后的数据）")
	flag.BoolVar(&config.Qtop, "qtop", false,
		"输出 Quattro top 层数据（仅用于 Quattro 格式）")
	flag.IntVar(&config.JPEGQuality, "jpg-quality", 98,
		"JPEG 质量 (1-100)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "x3f-go version %s\n", x3f.Version)
		fmt.Fprintf(os.Stderr, "Go implementation of X3F RAW converter\n\n")
		fmt.Fprintf(os.Stderr, "用法: x3f-go [选项] <输入.x3f>\n\n")
		fmt.Fprintf(os.Stderr, "选项:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n支持的输出格式:\n")
		fmt.Fprintf(os.Stderr, "  .dng   - DNG (Digital Negative, 线性 sRGB)\n")
		fmt.Fprintf(os.Stderr, "  .tiff  - TIFF (线性 sRGB)\n")
		fmt.Fprintf(os.Stderr, "  .jpg   - JPEG (带色调映射和伽马校正)\n")
		fmt.Fprintf(os.Stderr, "  .heif  - HEIF (高效图像格式)\n")
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

	// 检查 -c 参数只能用于 ppm 和 dng 格式
	if config.CompatibleWithC && outputExt != ".ppm" && outputExt != ".dng" {
		return fmt.Errorf("-c 参数只支持 PPM 和 DNG 格式，当前格式: %s", outputExt)
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
	case ".heif", ".heic":
		convertErr = convertToHEIF(x3fFile, config, logger)
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

func loadAndProcessImage(x3fFile *x3f.File, config *Config, opts x3f.ProcessOptions, logger *x3f.Logger) (*x3f.ProcessedImage, error) {
	// 查找 RAW 图像段
	var rawSection *x3f.ImageSection

	logger.Step("加载图像段")

	if config.Verbose {
		fmt.Printf("目录中共有 %d 个条目\n", len(x3fFile.Directory.Entries))
	}

	for i, entry := range x3fFile.Directory.Entries {
		// 对于 version >= 4.0，直接使用 IMA2/IMAG 类型
		// 对于 version < 4.0，使用 SECi 类型
		isImageSection := entry.Type == x3f.SECi ||
			entry.Type == x3f.IMA2 ||
			entry.Type == x3f.IMAG

		if config.Verbose {
			fmt.Printf("条目 %d: 类型=0x%08x, 偏移=%d, 长度=%d, 是图像段=%v\n",
				i, entry.Type, entry.Offset, entry.Length, isImageSection)
		}

		if isImageSection {
			if err := x3fFile.LoadImageSection(&entry); err != nil {
				if config.Verbose {
					fmt.Printf("警告: 无法加载图像段: %v\n", err)
				}
				continue
			}
			if config.Verbose && len(x3fFile.ImageData) > 0 {
				lastImg := x3fFile.ImageData[len(x3fFile.ImageData)-1]
				fmt.Printf("  -> 加载成功: %dx%d, Type=0x%08x, Format=0x%08x\n",
					lastImg.Columns, lastImg.Rows, lastImg.Type, lastImg.Format)
			}
		}
	}

	// 获取最新加载的图像段（通常是最后一个 RAW 图像）
	if len(x3fFile.ImageData) == 0 {
		if x3fFile.Header.Version >= 0x00040000 {
			return nil, fmt.Errorf("未找到可处理的图像数据\n\n"+
				"注意：这是一个 Quattro 格式文件 (版本 %d.%d)\n"+
				"Quattro RAW 格式目前暂不支持。\n\n"+
				"建议使用原始 C 版本的 x3f_extract 工具：\n"+
				"  ./bin/x3f_extract -unpack %s output_dir\n\n"+
				"详情请参阅 claude-docs/implementation-status.md",
				x3fFile.Header.Version>>16, x3fFile.Header.Version&0xFFFF,
				config.Input)
		}
		return nil, fmt.Errorf("未找到图像数据")
	}

	rawSection = x3fFile.ImageData[len(x3fFile.ImageData)-1]

	if config.Verbose {
		fmt.Printf("处理图像: %dx%d, 格式: 0x%08x\n",
			rawSection.Columns, rawSection.Rows, rawSection.Format)
		if rawSection.DecodedColumns > 0 || rawSection.DecodedRows > 0 {
			fmt.Printf("  解码尺寸: %dx%d\n",
				rawSection.DecodedColumns, rawSection.DecodedRows)
		}
	}

	logger.Done("完成")

	// 处理图像
	return x3f.ProcessRAW(x3fFile, rawSection, opts, logger)
}

func convertToDNG(x3fFile *x3f.File, config *Config, logger *x3f.Logger) error {
	// 使用统一的图像处理流程
	opts := x3f.ProcessOptions{
		WhiteBalance:     config.WhiteBalance,
		NoCrop:           config.NoCrop,
		IntermediateOnly: true, // DNG 只需要 intermediate 数据
	}

	preprocessed, _, err := x3f.ProcessImageUnified(x3fFile, opts, logger)
	if err != nil {
		return err
	}

	// 获取相机信息
	logger.Step("准备 DNG 元数据")
	cameraModel := "Sigma X3F"
	if model, ok := x3fFile.GetProperty("CAMMODEL"); ok {
		cameraModel = model
	}

	cameraSerial := ""
	if serial, ok := x3fFile.GetProperty("CAMSERIAL"); ok {
		cameraSerial = serial
	}

	// 获取白平衡
	wb := config.WhiteBalance
	if wb == "" {
		wb = x3fFile.GetWhiteBalance()
	}

	// 获取色彩矩阵
	colorMatrix, _ := x3fFile.GetColorMatrix(wb)

	// 获取白平衡增益 (仅在 C 兼容模式下使用)
	var whiteBalance x3f.Vector3
	if config.CompatibleWithC {
		var ok bool
		whiteBalance, ok = x3fFile.GetWhiteBalanceGain(wb)
		if !ok || (whiteBalance[0] == 0 && whiteBalance[1] == 0 && whiteBalance[2] == 0) {
			whiteBalance = x3f.DefaultWhiteBalanceGain
			if config.Verbose {
				fmt.Printf("使用默认白平衡增益: [%.5f, %.5f, %.5f]\n",
					whiteBalance[0], whiteBalance[1], whiteBalance[2])
			}
		}
	}

	// 获取 rawSection (需要用于 DNG 输出)
	rawSection := x3fFile.ImageData[len(x3fFile.ImageData)-1]

	dngOpts := output.DNGOptions{
		CameraModel:         cameraModel,
		CameraSerial:        cameraSerial,
		ColorMatrix:         colorMatrix,
		WhiteBalance:        whiteBalance,
		BaselineExpose:      1.0,
		LinearOutput:        true,
		NoCrop:              config.NoCrop,
		CompatibleWithC:     config.CompatibleWithC,
		IntermediateBias:    preprocessed.IntermediateBias,
		MaxIntermediate:     preprocessed.MaxIntermediate,
		HasIntermediateData: true,
		PreprocessedData:    preprocessed.Data,
		ImageWidth:          preprocessed.Width,
		ImageHeight:         preprocessed.Height,
	}

	logger.Done(cameraModel)

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

	// 使用统一的图像处理流程
	// TIFF 默认输出 sRGB（和 C 版本一致），应用 gamma 校正
	opts := x3f.ProcessOptions{
		WhiteBalance:      config.WhiteBalance,
		ColorSpace:        getColorSpace(config.ColorSpace),
		ApplyGamma:        true,                // 应用 gamma 校正（默认 sRGB）
		ToneMappingMethod: x3f.ToneMappingNone, // TIFF 默认不使用色调映射
		LinearOutput:      false,               // 输出 sRGB，不是线性
		NoCrop:            config.NoCrop,
		IntermediateOnly:  false, // TIFF 需要 RGB 数据
	}

	_, img, err := x3f.ProcessImageUnified(x3fFile, opts, logger)
	if err != nil {
		return err
	}

	// 从 X3F 文件中提取 EXIF 元数据
	make := "SIGMA"
	model := x3fFile.GetCameraModel()

	aperture, _ := x3fFile.GetCAMFFloat("CaptureAperture")
	shutter, _ := x3fFile.GetCAMFFloat("CaptureShutter")
	iso, _ := x3fFile.GetCAMFFloat("CaptureISO")

	// 快门速度是倒数形式（如 1740 表示 1/1740 秒）
	exposureTime := 0.0
	if shutter > 0 {
		exposureTime = 1.0 / shutter
	}

	tiffOpts := output.TIFFOptions{
		Use16Bit:     true,
		Make:         make,
		Model:        model,
		LensModel:    "", // X3F 文件中没有镜头型号信息
		FNumber:      aperture,
		ExposureTime: exposureTime,
		ISO:          uint16(iso),
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
	quality := config.JPEGQuality
	if quality < 1 || quality > 100 {
		return fmt.Errorf("JPEG 质量必须在 1-100 之间，当前值: %d", quality)
	}

	// 使用统一的图像处理流程
	// JPEG 在 x3f 包内完成所有 CPU 计算（并发），output 包只做 IO
	opts := x3f.ProcessOptions{
		WhiteBalance:      config.WhiteBalance,
		ColorSpace:        getColorSpace(config.ColorSpace),
		ApplyGamma:        true,                                     // 在 x3f 包内应用（并发）
		ToneMappingMethod: getToneMappingMethod(config.ToneMapping), // 在 x3f 包内应用（并发）
		LinearOutput:      false,
		NoCrop:            config.NoCrop,
		IntermediateOnly:  false, // JPEG 需要 RGB 数据
	}

	_, img, err := x3f.ProcessImageUnified(x3fFile, opts, logger)
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

func convertToHEIF(x3fFile *x3f.File, config *Config, logger *x3f.Logger) error {
	if config.Verbose {
		fmt.Println("转换为 HEIF...")
	}

	opts := x3f.ProcessOptions{
		WhiteBalance:      config.WhiteBalance,
		ColorSpace:        getColorSpace(config.ColorSpace),
		ApplyGamma:        true,
		ToneMappingMethod: getToneMappingMethod(config.ToneMapping),
		LinearOutput:      false,
		NoCrop:            config.NoCrop,
		IntermediateOnly:  false,
	}

	_, img, err := x3f.ProcessImageUnified(x3fFile, opts, logger)
	if err != nil {
		return err
	}

	heifOpts := output.HEIFOptions{
		Quality:  98,
		Use10Bit: true,
	}

	logger.Step("写入 HEIF")
	if config.Verbose {
		fmt.Printf("写入 HEIF 文件: %s\n", config.Output)
	}

	err = output.ExportHEIF(img, config.Output, heifOpts)
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

	// 先加载图像段
	for i, entry := range x3fFile.Directory.Entries {
		isImageSection := entry.Type == x3f.SECi ||
			entry.Type == x3f.IMA2 ||
			entry.Type == x3f.IMAG

		if config.Verbose {
			fmt.Printf("条目 %d: 类型=0x%08x, 是图像段=%v\n", i, entry.Type, isImageSection)
		}

		if isImageSection {
			if err := x3fFile.LoadImageSection(&entry); err != nil {
				if config.Verbose {
					fmt.Printf("警告: 无法加载图像段: %v\n", err)
				}
				continue
			}
		}
	}

	// 获取 RAW 图像段
	var rawSection *x3f.ImageSection
	for _, sec := range x3fFile.ImageData {
		if config.Verbose {
			fmt.Printf("检查图像段: Format=0x%08x\n", sec.Format)
		}
		if sec.Format == 0x0000001e || sec.Format == 0x00000023 || sec.Format == 0x00000012 {
			rawSection = sec
			break
		}
	}

	if rawSection == nil {
		return fmt.Errorf("未找到 RAW 图像段")
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
