package x3f

// ProcessOptions 预处理选项
// 用于 ProcessImage，包含预处理阶段的参数
type ProcessOptions struct {
	WhiteBalanceType string // 白平衡类型名称（如 "Auto", "Sunlight"）
	Denoise          bool   // 是否应用降噪
	NoCrop           bool   // 是否裁剪
}

// CameraInfo 相机信息和色彩配置
// 包含所有输出格式都需要的元数据和色彩信息
type CameraInfo struct {
	// 相机元数据
	Model  string // 相机型号
	Serial string // 相机序列号

	// 色彩信息
	ColorMatrix  Matrix3x3 // 色彩矩阵 (RAW → XYZ)
	WBGain       Vector3   // 白平衡增益
	WhiteBalance string    // 白平衡类型名称

	// 曝光信息
	BaselineExposure float64 // 基线曝光
}

// ExifInfo EXIF 拍摄参数
type ExifInfo struct {
	Make         string  // 制造商
	Model        string  // 相机型号
	LensModel    string  // 镜头型号
	FNumber      float64 // 光圈值
	ExposureTime float64 // 曝光时间（秒）
	ISO          uint16  // ISO 值
}

// ExtractCameraInfo 从 X3F 文件中提取相机信息
func ExtractCameraInfo(file *File, wb string) CameraInfo {
	info := CameraInfo{WhiteBalance: wb}

	// 获取相机型号
	if model, ok := file.GetProperty("CAMMODEL"); ok {
		info.Model = model
	} else {
		info.Model = "Sigma X3F"
	}

	// 获取相机序列号
	if serial, ok := file.GetProperty("CAMSERIAL"); ok {
		info.Serial = serial
	}

	// 获取色彩矩阵
	if matrix, ok := file.GetColorMatrix(wb); ok {
		info.ColorMatrix = matrix
	} else {
		info.ColorMatrix = Identity3x3()
	}

	// 获取白平衡增益
	if gain, ok := file.GetWhiteBalanceGain(wb); ok {
		info.WBGain = gain
	} else {
		info.WBGain = DefaultWhiteBalanceGain
	}

	// 默认基线曝光
	info.BaselineExposure = 1.0

	return info
}

// ExtractExifInfo 从 X3F 文件中提取 EXIF 拍摄参数
func ExtractExifInfo(file *File) ExifInfo {
	exif := ExifInfo{
		Make: "SIGMA",
	}

	// 获取相机型号
	if model, ok := file.GetProperty("CAMMODEL"); ok {
		exif.Model = model
	} else {
		exif.Model = file.GetCameraModel()
	}

	// 获取拍摄参数
	if aperture, ok := file.GetCAMFFloat("CaptureAperture"); ok {
		exif.FNumber = aperture
	}

	if shutter, ok := file.GetCAMFFloat("CaptureShutter"); ok {
		if shutter > 0 {
			exif.ExposureTime = 1.0 / shutter // 快门速度是倒数形式
		}
	}

	if iso, ok := file.GetCAMFFloat("CaptureISO"); ok {
		exif.ISO = uint16(iso)
	}

	// X3F 文件中没有镜头型号信息
	exif.LensModel = ""

	return exif
}
