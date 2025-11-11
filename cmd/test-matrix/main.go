package main

import (
	"fmt"
	"math"
)

// multiply3x3 3x3 矩阵相乘（当前实现）
func multiply3x3(a, b []float64) []float64 {
	if len(a) != 9 || len(b) != 9 {
		return make([]float64, 9)
	}

	result := make([]float64, 9)
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			sum := 0.0
			for k := 0; k < 3; k++ {
				sum += a[i*3+k] * b[k*3+j]
			}
			result[i*3+j] = sum
		}
	}
	return result
}

// multiply3x3Transpose 尝试转置后相乘
func multiply3x3Transpose(a, b []float64) []float64 {
	if len(a) != 9 || len(b) != 9 {
		return make([]float64, 9)
	}

	// 将 b 转置
	bT := []float64{
		b[0], b[3], b[6],
		b[1], b[4], b[7],
		b[2], b[5], b[8],
	}

	result := make([]float64, 9)
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			sum := 0.0
			for k := 0; k < 3; k++ {
				sum += a[i*3+k] * bT[k*3+j]
			}
			result[i*3+j] = sum
		}
	}
	return result
}

// multiply3x3_v2 另一种实现（b先取出一列）
func multiply3x3_v2(a, b []float64) []float64 {
	if len(a) != 9 || len(b) != 9 {
		return make([]float64, 9)
	}

	result := make([]float64, 9)
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			sum := 0.0
			for k := 0; k < 3; k++ {
				sum += a[i*3+k] * b[j+k*3]
			}
			result[i*3+j] = sum
		}
	}
	return result
}

func printMatrix(name string, m []float64) {
	fmt.Printf("%s:\n", name)
	fmt.Printf("  [%.6f, %.6f, %.6f]\n", m[0], m[1], m[2])
	fmt.Printf("  [%.6f, %.6f, %.6f]\n", m[3], m[4], m[5])
	fmt.Printf("  [%.6f, %.6f, %.6f]\n", m[6], m[7], m[8])
	fmt.Println()
}

func main() {
	fmt.Println("=== 矩阵乘法测试 ===")
	fmt.Println()

	// 从 CAMF 读取的数据
	sRGBToXYZ := []float64{
		0.4124564, 0.3575761, 0.1804375,
		0.2126729, 0.7151522, 0.0721750,
		0.0193339, 0.1191920, 0.9503041,
	}

	ccMatrix := []float64{
		3.953094, -4.281235, 1.328171,
		-2.156204, 3.913986, -0.757812,
		0.914078, -4.781235, 4.867157,
	}

	// C 版本的期望结果（从 ForwardMatrix1 反推，去掉 D65_to_D50）
	// 我们先用 C 版本的 ForwardMatrix1 来验证
	forwardMatrix1 := []float64{
		1.024114728, -1.043815374, 0.9839450717,
		-0.6111185551, 1.56382525, 0.04727615416,
		0.4985253513, -3.094777584, 3.421587229,
	}

	// D65_to_D50 矩阵
	d65ToD50 := []float64{
		1.0478112, 0.0228866, -0.0501270,
		0.0295424, 0.9904844, -0.0170491,
		-0.0092345, 0.0150436, 0.7521316,
	}

	printMatrix("sRGB_to_XYZ", sRGBToXYZ)
	printMatrix("ccMatrix (ColorCorrections)", ccMatrix)
	printMatrix("ForwardMatrix1 (C版本)", forwardMatrix1)
	printMatrix("D65_to_D50", d65ToD50)

	fmt.Println("=== 测试不同的矩阵乘法实现 ===")
	fmt.Println()

	// 测试 1: 当前实现
	result1 := multiply3x3(sRGBToXYZ, ccMatrix)
	printMatrix("方法1 (当前): sRGB_to_XYZ × ccMatrix", result1)

	// 测试 2: 转置后相乘
	result2 := multiply3x3Transpose(sRGBToXYZ, ccMatrix)
	printMatrix("方法2 (ccMatrix转置): sRGB_to_XYZ × ccMatrix^T", result2)

	// 测试 3: 第二种实现
	result3 := multiply3x3_v2(sRGBToXYZ, ccMatrix)
	printMatrix("方法3 (改变索引顺序): sRGB_to_XYZ × ccMatrix", result3)

	// 测试 4: 交换顺序
	result4 := multiply3x3(ccMatrix, sRGBToXYZ)
	printMatrix("方法4 (交换顺序): ccMatrix × sRGB_to_XYZ", result4)

	// 从 ForwardMatrix1 反推 bmt_to_xyz
	// ForwardMatrix1 = D65_to_D50 × bmt_to_xyz
	// 所以 bmt_to_xyz = inverse(D65_to_D50) × ForwardMatrix1

	// 计算 D65_to_D50 的逆矩阵
	d65ToD50Inv := inverse3x3(d65ToD50)
	printMatrix("inverse(D65_to_D50)", d65ToD50Inv)

	// 反推 bmt_to_xyz
	bmtToXYZ_expected := multiply3x3(d65ToD50Inv, forwardMatrix1)
	printMatrix("期望的 bmt_to_xyz (从C版本反推)", bmtToXYZ_expected)

	fmt.Println("=== 比较结果 ===")
	fmt.Println()

	// 计算与期望结果的差异
	compareMatrices := func(name string, m []float64) {
		maxDiff := 0.0
		for i := 0; i < 9; i++ {
			diff := math.Abs(m[i] - bmtToXYZ_expected[i])
			if diff > maxDiff {
				maxDiff = diff
			}
		}
		fmt.Printf("%s 最大差异: %.10f\n", name, maxDiff)
	}

	compareMatrices("方法1", result1)
	compareMatrices("方法2", result2)
	compareMatrices("方法3", result3)
	compareMatrices("方法4", result4)

	fmt.Println()
	fmt.Println("=== 验证 ColorMatrix1 ===")
	fmt.Println()

	// C 版本的 ColorMatrix1
	colorMatrix1_C := []float64{
		3.240625381, -1.537207961, -0.4986285865,
		-0.9689307213, 1.875756025, 0.04151752219,
		0.05571012199, -0.2040210515, 1.056995988,
	}
	printMatrix("ColorMatrix1 (C版本)", colorMatrix1_C)

	// 从我们计算的 bmt_to_xyz 求逆得到 ColorMatrix1
	colorMatrix1_Go := inverse3x3(result1)
	printMatrix("ColorMatrix1 (Go计算): inverse(bmt_to_xyz)", colorMatrix1_Go)

	// 从期望的 bmt_to_xyz 求逆
	colorMatrix1_Expected := inverse3x3(bmtToXYZ_expected)
	printMatrix("ColorMatrix1 (期望): inverse(期望的bmt_to_xyz)", colorMatrix1_Expected)

	// 比较差异
	maxDiff_Go := 0.0
	maxDiff_Expected := 0.0
	for i := 0; i < 9; i++ {
		diff1 := math.Abs(colorMatrix1_Go[i] - colorMatrix1_C[i])
		diff2 := math.Abs(colorMatrix1_Expected[i] - colorMatrix1_C[i])
		if diff1 > maxDiff_Go {
			maxDiff_Go = diff1
		}
		if diff2 > maxDiff_Expected {
			maxDiff_Expected = diff2
		}
	}

	fmt.Printf("Go 计算的 ColorMatrix1 与 C 版本差异: %.10f\n", maxDiff_Go)
	fmt.Printf("期望的 ColorMatrix1 与 C 版本差异: %.10f\n", maxDiff_Expected)

	fmt.Println()
	fmt.Println("=== 分析 DNG 色彩转换流程 ===")
	fmt.Println()

	// CameraCalibration1 (从 DNG 文件读取)
	cameraCalib1 := []float64{
		0.4411287, 0, 0,
		0, 0.8693692, 0,
		0, 0, 1.482869,
	}
	printMatrix("CameraCalibration1 (DNG)", cameraCalib1)

	// 根据 DNG 规范，实际的转换矩阵是：
	// CameraToXYZ = ColorMatrix1 × CameraCalibration1
	// 但 ColorMatrix1 在 DNG 中的定义比较特殊...

	// 让我们计算 inverse(ColorMatrix1_C) 看看
	invColorMatrix1_C := inverse3x3(colorMatrix1_C)
	printMatrix("inverse(ColorMatrix1_C)", invColorMatrix1_C)

	// 比较 inverse(ColorMatrix1_C) 和我们计算的 bmt_to_xyz
	maxDiff_inv := 0.0
	for i := 0; i < 9; i++ {
		diff := math.Abs(invColorMatrix1_C[i] - result1[i])
		if diff > maxDiff_inv {
			maxDiff_inv = diff
		}
	}
	fmt.Printf("\ninverse(ColorMatrix1_C) 与 Go的bmt_to_xyz 差异: %.10f\n", maxDiff_inv)
}

func inverse3x3(m []float64) []float64 {
	if len(m) != 9 {
		return make([]float64, 9)
	}

	inv := make([]float64, 9)

	// 计算行列式
	det := m[0]*(m[4]*m[8]-m[5]*m[7]) -
		m[1]*(m[3]*m[8]-m[5]*m[6]) +
		m[2]*(m[3]*m[7]-m[4]*m[6])

	if math.Abs(det) < 1e-10 {
		// 矩阵奇异，返回单位矩阵
		inv[0], inv[4], inv[8] = 1, 1, 1
		return inv
	}

	invDet := 1.0 / det

	inv[0] = (m[4]*m[8] - m[5]*m[7]) * invDet
	inv[1] = (m[2]*m[7] - m[1]*m[8]) * invDet
	inv[2] = (m[1]*m[5] - m[2]*m[4]) * invDet
	inv[3] = (m[5]*m[6] - m[3]*m[8]) * invDet
	inv[4] = (m[0]*m[8] - m[2]*m[6]) * invDet
	inv[5] = (m[2]*m[3] - m[0]*m[5]) * invDet
	inv[6] = (m[3]*m[7] - m[4]*m[6]) * invDet
	inv[7] = (m[1]*m[6] - m[0]*m[7]) * invDet
	inv[8] = (m[0]*m[4] - m[1]*m[3]) * invDet

	return inv
}
