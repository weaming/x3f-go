package colorspace

// 标准色彩空间定义

// D65 白点 (CIE 标准光源 D65)
var D65WhitePoint = Vector3{0.95047, 1.0, 1.08883}

// D50 白点
var D50WhitePoint = Vector3{0.96422, 1.0, 0.82521}

// sRGB 到 XYZ (D65) 的转换矩阵
var SRGBToXYZ = Matrix3x3{
	0.4124564, 0.3575761, 0.1804375,
	0.2126729, 0.7151522, 0.0721750,
	0.0193339, 0.1191920, 0.9503041,
}

// XYZ (D65) 到 sRGB 的转换矩阵
var XYZToSRGB = Matrix3x3{
	3.2404542, -1.5371385, -0.4985314,
	-0.9692660, 1.8760108, 0.0415560,
	0.0556434, -0.2040259, 1.0572252,
}

// Adobe RGB 到 XYZ (D65) 的转换矩阵
var AdobeRGBToXYZ = Matrix3x3{
	0.5767309, 0.1855540, 0.1881852,
	0.2973769, 0.6273491, 0.0752741,
	0.0270343, 0.0706872, 0.9911085,
}

// XYZ (D65) 到 Adobe RGB 的转换矩阵
var XYZToAdobeRGB = Matrix3x3{
	2.0413690, -0.5649464, -0.3446944,
	-0.9692660, 1.8760108, 0.0415560,
	0.0134474, -0.1183897, 1.0154096,
}

// ProPhoto RGB 到 XYZ (D50) 的转换矩阵
var ProPhotoRGBToXYZ = Matrix3x3{
	0.7976749, 0.1351917, 0.0313534,
	0.2880402, 0.7118741, 0.0000857,
	0.0000000, 0.0000000, 0.8252100,
}

// XYZ (D50) 到 ProPhoto RGB 的转换矩阵
var XYZToProPhotoRGB = Matrix3x3{
	1.3459433, -0.2556075, -0.0511118,
	-0.5445989, 1.5081673, 0.0205351,
	0.0000000, 0.0000000, 1.2118128,
}

// Bradford 色适应矩阵 (D65 → D50)
var BradfordD65ToD50 = Matrix3x3{
	1.0478112, 0.0228866, -0.0501270,
	0.0295424, 0.9904844, -0.0170491,
	-0.0092345, 0.0150436, 0.7521316,
}

// Bradford 色适应矩阵 (D50 → D65)
var BradfordD50ToD65 = Matrix3x3{
	0.9555766, -0.0230393, 0.0631636,
	-0.0282895, 1.0099416, 0.0210077,
	0.0122982, -0.0204830, 1.3299098,
}

// ColorSpace 色彩空间类型
type ColorSpace int

const (
	// ColorSpaceNone 无色彩转换
	ColorSpaceNone ColorSpace = iota
	// ColorSpaceSRGB sRGB 色彩空间
	ColorSpaceSRGB
	// ColorSpaceAdobeRGB Adobe RGB 色彩空间
	ColorSpaceAdobeRGB
	// ColorSpaceProPhotoRGB ProPhoto RGB 色彩空间
	ColorSpaceProPhotoRGB
)

// GetXYZToRGBMatrix 获取 XYZ → RGB 转换矩阵
func GetXYZToRGBMatrix(cs ColorSpace) Matrix3x3 {
	switch cs {
	case ColorSpaceSRGB:
		return XYZToSRGB
	case ColorSpaceAdobeRGB:
		return XYZToAdobeRGB
	case ColorSpaceProPhotoRGB:
		// ProPhoto RGB 使用 D50，需要先转换
		return XYZToProPhotoRGB.Multiply(BradfordD65ToD50)
	default:
		return Identity3x3()
	}
}

// GetRGBToXYZMatrix 获取 RGB → XYZ 转换矩阵
func GetRGBToXYZMatrix(cs ColorSpace) Matrix3x3 {
	switch cs {
	case ColorSpaceSRGB:
		return SRGBToXYZ
	case ColorSpaceAdobeRGB:
		return AdobeRGBToXYZ
	case ColorSpaceProPhotoRGB:
		// ProPhoto RGB 使用 D50，需要先转换
		return BradfordD50ToD65.Multiply(ProPhotoRGBToXYZ)
	default:
		return Identity3x3()
	}
}

// GetGamma 获取色彩空间的 gamma 值
func GetGamma(cs ColorSpace) float64 {
	switch cs {
	case ColorSpaceSRGB, ColorSpaceAdobeRGB:
		return 2.2
	case ColorSpaceProPhotoRGB:
		return 1.8
	default:
		return 1.0
	}
}
