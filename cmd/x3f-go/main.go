package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/weaming/x3f-go/colorspace"
	"github.com/weaming/x3f-go/output"
	"github.com/weaming/x3f-go/processor"
	"github.com/weaming/x3f-go/x3f"
)

type Config struct {
	Input           string
	Output          string
	ColorSpace      string
	WhiteBalance    string
	Verbose         bool
	ShowVersion     bool
	NoCrop          bool
	CompatibleWithC bool
	DumpMeta        bool
	Unprocessed     bool
}

func main() {
	config := parseFlags()

	if config.ShowVersion {
		fmt.Printf("x3f-go version %s\n", output.Version)
		fmt.Println("Go implementation of X3F RAW converter")
		os.Exit(0)
	}

	if config.Input == "" {
		fmt.Fprintln(os.Stderr, "错误: 必须指定输入文件")
		flag.Usage()
		os.Exit(1)
	}

	if config.Output == "" && !config.DumpMeta {
		fmt.Fprintln(os.Stderr, "错误: 必须指定输出文件 (-o) 或使用 -meta")
		flag.Usage()
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
	flag.BoolVar(&config.Verbose, "v", false,
		"详细输出")
	flag.BoolVar(&config.ShowVersion, "version", false,
		"显示版本信息")
	flag.BoolVar(&config.NoCrop, "no-crop", false,
		"不裁剪，输出完整解码数据")
	flag.BoolVar(&config.CompatibleWithC, "c", false,
		"C 兼容模式：生成与 C 版本完全相同的输出（完整图像+Active Area）")
	flag.BoolVar(&config.DumpMeta, "meta", false,
		"输出元数据到 <输入文件>.meta")
	flag.BoolVar(&config.Unprocessed, "unprocessed", false,
		"输出未处理的原始 RAW 数据（默认输出预处理后的数据）")

	flag.Usage = func() {
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
	if config.Verbose {
		fmt.Printf("打开 X3F 文件: %s\n", config.Input)
	}

	// Open X3F file
	x3fFile, err := x3f.Open(config.Input)
	if err != nil {
		return fmt.Errorf("无法打开 X3F 文件: %w", err)
	}
	defer x3fFile.Close()

	if config.Verbose {
		fmt.Printf("X3F 版本: 0x%08x\n", x3fFile.Header.Version)
		fmt.Printf("文件头尺寸: %dx%d\n", x3fFile.Header.Columns, x3fFile.Header.Rows)
	}

	// Load required sections
	if err := x3fFile.LoadSection(x3f.SECp); err != nil && config.Verbose {
		fmt.Printf("警告: 无法加载属性段: %v\n", err)
	}

	// CAMF 段目前不支持解码，所以会失败
	// 这不影响基本的图像提取功能
	if err := x3fFile.LoadSection(x3f.SECc); err != nil {
		if config.Verbose {
			fmt.Printf("注意: CAMF 段未加载 (需要解码支持): %v\n", err)
		}
	} else if config.Verbose {
		fmt.Printf("CAMF 段加载成功\n")
	}

	// 如果指定了 -meta,输出元数据并退出
	if config.DumpMeta {
		return dumpMetadata(x3fFile, config)
	}

	// Determine output format from extension
	outputExt := strings.ToLower(filepath.Ext(config.Output))

	if config.Verbose {
		fmt.Printf("输出格式: %s\n", outputExt)
		fmt.Printf("色彩空间: %s\n", config.ColorSpace)
		fmt.Printf("白平衡: %s\n", config.WhiteBalance)
	}

	switch outputExt {
	case ".dng":
		return convertToDNG(x3fFile, config)
	case ".tiff", ".tif":
		return convertToTIFF(x3fFile, config)
	case ".jpg", ".jpeg":
		return convertToJPEG(x3fFile, config)
	case ".heif", ".heic":
		return convertToHEIF(x3fFile, config)
	case ".ppm":
		return convertToPPM(x3fFile, config)
	default:
		return fmt.Errorf("不支持的输出格式: %s (支持: .dng, .tiff, .jpg, .heif, .ppm)", outputExt)
	}
}

func getColorSpace(name string) colorspace.ColorSpace {
	switch strings.ToLower(name) {
	case "none":
		return colorspace.ColorSpaceNone
	case "srgb":
		return colorspace.ColorSpaceSRGB
	case "adobergb":
		return colorspace.ColorSpaceAdobeRGB
	case "prophoto", "prophotorgb":
		return colorspace.ColorSpaceProPhotoRGB
	default:
		return colorspace.ColorSpaceSRGB
	}
}

func loadAndProcessImage(x3fFile *x3f.File, config *Config, opts processor.ProcessOptions) (*processor.ProcessedImage, error) {
	// 查找 RAW 图像段
	var rawSection *x3f.ImageSection

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

	// 处理图像
	return processor.ProcessRAW(x3fFile, rawSection, opts)
}

func convertToDNG(x3fFile *x3f.File, config *Config) error {
	if config.Verbose {
		fmt.Println("转换为 DNG...")
	}

	// DNG 应该输出未经色彩处理的线性 RAW 数据
	// 直接从解码后的图像段获取数据,不经过 ProcessRAW 的色彩转换

	// 查找 RAW 图像段
	var rawSection *x3f.ImageSection
	for _, entry := range x3fFile.Directory.Entries {
		isImageSection := entry.Type == x3f.SECi ||
			entry.Type == x3f.IMA2 ||
			entry.Type == x3f.IMAG

		if isImageSection {
			if err := x3fFile.LoadImageSection(&entry); err != nil {
				if config.Verbose {
					fmt.Printf("警告: 无法加载图像段: %v\n", err)
				}
				continue
			}
		}
	}

	if len(x3fFile.ImageData) == 0 {
		return fmt.Errorf("未找到图像数据")
	}

	rawSection = x3fFile.ImageData[len(x3fFile.ImageData)-1]

	// 解码图像
	if rawSection.DecodedData == nil {
		if err := rawSection.DecodeImage(); err != nil {
			return fmt.Errorf("解码图像失败: %w", err)
		}
	}

	// 获取白平衡
	wb := config.WhiteBalance
	if wb == "" {
		wb = x3fFile.GetWhiteBalance()
	}

	// 在预处理之前,计算 intermediate 数据的电平信息(用于 linear sRGB 转换)
	var intermediateBias float64
	var maxIntermediate [3]uint32
	hasIntermediateData := false

	if !config.CompatibleWithC {
		blackLevel, err := processor.CalculateBlackLevel(x3fFile, rawSection)
		if err == nil {
			if bias, ok := processor.GetIntermediateBias(x3fFile, wb, blackLevel); ok {
				intermediateBias = bias
				if maxInt, ok2 := processor.GetMaxIntermediate(x3fFile, wb, intermediateBias); ok2 {
					maxIntermediate = maxInt
					hasIntermediateData = true
				}
			}
		}
		if !hasIntermediateData && config.Verbose {
			fmt.Printf("警告: 无法计算 intermediate levels\n")
		}
	}

	// 应用预处理 (黑电平校正、intermediate bias、scale 转换)
	// 将 12-bit RAW 转换为 14-bit intermediate 数据
	if err := processor.PreprocessData(x3fFile, rawSection, wb); err != nil && config.Verbose {
		fmt.Printf("警告: 预处理失败: %v\n", err)
	}

	// 获取相机信息
	cameraModel := "Sigma X3F"
	if model, ok := x3fFile.GetProperty("CAMMODEL"); ok {
		cameraModel = model
	}

	cameraSerial := ""
	if serial, ok := x3fFile.GetProperty("CAMSERIAL"); ok {
		cameraSerial = serial
	}

	// 获取色彩矩阵
	colorMatrix, _ := x3fFile.GetColorMatrix(wb)

	// 获取白平衡增益 (仅在 C 兼容模式下使用)
	var whiteBalance [3]float64
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

	dngOpts := output.DNGOptions{
		CameraModel:         cameraModel,
		CameraSerial:        cameraSerial,
		ColorMatrix:         colorMatrix,
		WhiteBalance:        whiteBalance,
		BaselineExpose:      1.0,
		LinearOutput:        true,
		NoCrop:              config.NoCrop,
		CompatibleWithC:     config.CompatibleWithC,
		IntermediateBias:    intermediateBias,
		MaxIntermediate:     maxIntermediate,
		HasIntermediateData: hasIntermediateData,
	}

	if config.Verbose {
		fmt.Printf("写入 DNG 文件: %s\n", config.Output)
	}

	return output.ExportRawDNG(x3fFile, rawSection, config.Output, dngOpts)
}

func convertToTIFF(x3fFile *x3f.File, config *Config) error {
	if config.Verbose {
		fmt.Println("转换为 TIFF...")
	}

	opts := processor.ProcessOptions{
		WhiteBalance: config.WhiteBalance,
		ColorSpace:   getColorSpace(config.ColorSpace),
		ApplyGamma:   false, // TIFF 使用线性输出
		ToneMapping:  false,
		LinearOutput: true,
		NoCrop:       config.NoCrop,
	}

	img, err := loadAndProcessImage(x3fFile, config, opts)
	if err != nil {
		return err
	}

	tiffOpts := output.TIFFOptions{
		Use16Bit: true,
	}

	if config.Verbose {
		fmt.Printf("写入 TIFF 文件: %s\n", config.Output)
	}

	return output.ExportTIFF(img, config.Output, tiffOpts)
}

func convertToJPEG(x3fFile *x3f.File, config *Config) error {
	if config.Verbose {
		fmt.Println("转换为 JPEG...")
	}

	opts := processor.ProcessOptions{
		WhiteBalance: config.WhiteBalance,
		ColorSpace:   getColorSpace(config.ColorSpace),
		ApplyGamma:   false, // JPEG 输出时手动应用
		ToneMapping:  false, // JPEG 输出时手动应用
		LinearOutput: false,
		NoCrop:       config.NoCrop,
	}

	img, err := loadAndProcessImage(x3fFile, config, opts)
	if err != nil {
		return err
	}

	jpegOpts := output.JPEGOptions{
		Quality:     95,
		ApplyGamma:  true,
		ToneMapping: true,
	}

	if config.Verbose {
		fmt.Printf("写入 JPEG 文件: %s\n", config.Output)
	}

	return output.ExportJPEG(img, config.Output, jpegOpts)
}

func convertToHEIF(x3fFile *x3f.File, config *Config) error {
	if config.Verbose {
		fmt.Println("转换为 HEIF...")
	}

	opts := processor.ProcessOptions{
		WhiteBalance: config.WhiteBalance,
		ColorSpace:   getColorSpace(config.ColorSpace),
		ApplyGamma:   false,
		ToneMapping:  false,
		LinearOutput: false,
		NoCrop:       config.NoCrop,
	}

	img, err := loadAndProcessImage(x3fFile, config, opts)
	if err != nil {
		return err
	}

	heifOpts := output.HEIFOptions{
		Quality:     90,
		ApplyGamma:  true,
		ToneMapping: true,
		Use10Bit:    true,
	}

	if config.Verbose {
		fmt.Printf("写入 HEIF 文件: %s\n", config.Output)
	}

	return output.ExportHEIF(img, config.Output, heifOpts)
}

func convertToPPM(x3fFile *x3f.File, config *Config) error {
	if config.Unprocessed {
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

	if config.Unprocessed {
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
