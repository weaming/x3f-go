package matrix

// Matrix3x3 表示 3x3 矩阵
type Matrix3x3 [9]float64

// Matrix3x1 表示 3x1 向量
type Matrix3x1 [3]float64

// Multiply3x3 计算两个 3x3 矩阵相乘
func Multiply3x3(a, b Matrix3x3) Matrix3x3 {
	var c Matrix3x3
	c[0] = a[0]*b[0] + a[1]*b[3] + a[2]*b[6]
	c[1] = a[0]*b[1] + a[1]*b[4] + a[2]*b[7]
	c[2] = a[0]*b[2] + a[1]*b[5] + a[2]*b[8]

	c[3] = a[3]*b[0] + a[4]*b[3] + a[5]*b[6]
	c[4] = a[3]*b[1] + a[4]*b[4] + a[5]*b[7]
	c[5] = a[3]*b[2] + a[4]*b[5] + a[5]*b[8]

	c[6] = a[6]*b[0] + a[7]*b[3] + a[8]*b[6]
	c[7] = a[6]*b[1] + a[7]*b[4] + a[8]*b[7]
	c[8] = a[6]*b[2] + a[7]*b[5] + a[8]*b[8]
	return c
}

// Inverse3x3 计算 3x3 矩阵的逆
func Inverse3x3(a Matrix3x3) Matrix3x3 {
	var ainv Matrix3x3

	A := +(a[4]*a[8] - a[5]*a[7])
	B := -(a[3]*a[8] - a[5]*a[6])
	C := +(a[3]*a[7] - a[4]*a[6])

	D := -(a[1]*a[8] - a[2]*a[7])
	E := +(a[0]*a[8] - a[2]*a[6])
	F := -(a[0]*a[7] - a[1]*a[6])

	G := +(a[1]*a[5] - a[2]*a[4])
	H := -(a[0]*a[5] - a[2]*a[3])
	I := +(a[0]*a[4] - a[1]*a[3])

	det := a[0]*A + a[1]*B + a[2]*C

	ainv[0] = A / det
	ainv[1] = D / det
	ainv[2] = G / det
	ainv[3] = B / det
	ainv[4] = E / det
	ainv[5] = H / det
	ainv[6] = C / det
	ainv[7] = F / det
	ainv[8] = I / det

	return ainv
}

// SRGBToXYZ 返回 sRGB 到 XYZ 色彩空间的转换矩阵
func SRGBToXYZ() Matrix3x3 {
	return Matrix3x3{
		0.4124, 0.3576, 0.1805,
		0.2126, 0.7152, 0.0722,
		0.0193, 0.1192, 0.9505,
	}
}

// BradfordD65ToD50 返回 Bradford 色彩适应矩阵 (D65 -> D50)
func BradfordD65ToD50() Matrix3x3 {
	return Matrix3x3{
		+1.0478112, +0.0228866, -0.0501270,
		+0.0295424, +0.9904844, -0.0170491,
		-0.0092345, +0.0150436, +0.7521316,
	}
}
