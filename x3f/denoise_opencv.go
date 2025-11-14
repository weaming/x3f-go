package x3f

/*
#cgo CPPFLAGS: -I${SRCDIR}/../build/opencv-install/include/opencv4
#cgo CXXFLAGS: -std=c++11

// 库搜索路径
#cgo LDFLAGS: -L${SRCDIR}/../build/opencv-install/lib
#cgo LDFLAGS: -L${SRCDIR}/../build/opencv-install/lib/opencv4/3rdparty

// OpenCV 静态库（顺序很重要：photo 依赖 imgproc，imgproc 依赖 core）
#cgo LDFLAGS: -lopencv_photo -lopencv_imgproc -lopencv_core

// OpenCV 第三方依赖
#cgo LDFLAGS: -llibopenjp2 -lzlib

// macOS 特定依赖
#cgo darwin LDFLAGS: -framework Accelerate -framework CoreFoundation

// Linux 特定依赖
#cgo linux LDFLAGS: -lpthread -ldl -lrt

// Windows 特定依赖
#cgo windows LDFLAGS: -lgdi32 -lole32 -loleaut32

#include <stdint.h>

// 声明 C++ 实现的函数（在 denoise_opencv.cpp 中）
void denoise_nlm_opencv(uint16_t* data, int rows, int cols, int channels, int rowStride, float h);
void denoise_quattro_highres_opencv(uint16_t* data, int rows, int cols, int channels, int rowStride, float h);
void bicubic_upscale_opencv(uint16_t* src, int srcRows, int srcCols, int channels, int srcStride,
                            uint16_t* dst, int dstRows, int dstCols, int dstStride);
void inpaint_bad_pixels_opencv(uint16_t* data, int rows, int cols, int channels, int rowStride,
                               uint8_t* mask, int maskStride, int inpaintRadius, int method);
*/
import "C"
import (
	"unsafe"
)

// BicubicUpscaleOpenCV 使用 OpenCV 进行 Bicubic 上采样（与 C 版本完全一致）
func BicubicUpscaleOpenCV(src []uint16, srcRows, srcCols, channels, srcStride int,
	dst []uint16, dstRows, dstCols, dstStride int) {
	if len(src) == 0 || len(dst) == 0 {
		return
	}
	C.bicubic_upscale_opencv(
		(*C.uint16_t)(unsafe.Pointer(&src[0])),
		C.int(srcRows), C.int(srcCols), C.int(channels), C.int(srcStride),
		(*C.uint16_t)(unsafe.Pointer(&dst[0])),
		C.int(dstRows), C.int(dstCols), C.int(dstStride),
	)
}

// DenoiseWithOpenCV 使用 OpenCV 进行降噪（支持 stride）
// 执行完整的三步降噪：主降噪 + V median + 低频降噪
func DenoiseWithOpenCV(data []uint16, rows, cols, channels, rowStride int, h float64) {
	if len(data) == 0 {
		return
	}
	C.denoise_nlm_opencv(
		(*C.uint16_t)(unsafe.Pointer(&data[0])),
		C.int(rows),
		C.int(cols),
		C.int(channels),
		C.int(rowStride),
		C.float(h),
	)
}

// DenoiseQuattroHighRes 使用 OpenCV 进行 Quattro 高分辨率降噪
// 只执行一次 fastNlMeansDenoising，V 通道使用 h*2 强度
func DenoiseQuattroHighRes(data []uint16, rows, cols, channels, rowStride int, h float64) {
	if len(data) == 0 {
		return
	}
	C.denoise_quattro_highres_opencv(
		(*C.uint16_t)(unsafe.Pointer(&data[0])),
		C.int(rows),
		C.int(cols),
		C.int(channels),
		C.int(rowStride),
		C.float(h),
	)
}

// InpaintMethod 定义 inpaint 算法类型
type InpaintMethod int

const (
	// InpaintNS Navier-Stokes 算法（质量好但慢）
	InpaintNS InpaintMethod = 0
	// InpaintTELEA 快速行进法（快但质量略差）
	InpaintTELEA InpaintMethod = 1
)

// InpaintBadPixelsOpenCV 使用 OpenCV 的 inpaint 算法修复坏点
// data: 图像数据 (uint16)
// mask: 坏点掩码 (uint8，非零处表示坏点)
// radius: 修复半径（通常为 3）
// method: 修复算法 (InpaintNS 或 InpaintTELEA)
func InpaintBadPixelsOpenCV(data []uint16, rows, cols, channels, rowStride int,
	mask []uint8, maskStride, radius int, method InpaintMethod) {
	if len(data) == 0 || len(mask) == 0 {
		return
	}
	C.inpaint_bad_pixels_opencv(
		(*C.uint16_t)(unsafe.Pointer(&data[0])),
		C.int(rows),
		C.int(cols),
		C.int(channels),
		C.int(rowStride),
		(*C.uint8_t)(unsafe.Pointer(&mask[0])),
		C.int(maskStride),
		C.int(radius),
		C.int(method),
	)
}
