package x3f

import "math"

// Matrix3x3 表示 3x3 矩阵（行优先存储）
type Matrix3x3 [9]float64

// Vector3 表示 3 维向量
type Vector3 [3]float64

// 矩阵乘法 (this * other)
func (m Matrix3x3) Multiply(other Matrix3x3) Matrix3x3 {
	var result Matrix3x3

	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			sum := 0.0
			for k := 0; k < 3; k++ {
				sum += m[i*3+k] * other[k*3+j]
			}
			result[i*3+j] = sum
		}
	}

	return result
}

// 应用矩阵到向量 (matrix * vector)
func (m Matrix3x3) Apply(v Vector3) Vector3 {
	return Vector3{
		m[0]*v[0] + m[1]*v[1] + m[2]*v[2],
		m[3]*v[0] + m[4]*v[1] + m[5]*v[2],
		m[6]*v[0] + m[7]*v[1] + m[8]*v[2],
	}
}

// 计算矩阵的逆
func (m Matrix3x3) Inverse() (Matrix3x3, error) {
	var inv Matrix3x3

	// 计算行列式
	det := m[0]*(m[4]*m[8]-m[5]*m[7]) -
		m[1]*(m[3]*m[8]-m[5]*m[6]) +
		m[2]*(m[3]*m[7]-m[4]*m[6])

	if math.Abs(det) < 1e-10 {
		// 返回单位矩阵
		return Identity3x3(), nil
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

	return inv, nil
}

// 转置矩阵
func (m Matrix3x3) Transpose() Matrix3x3 {
	return Matrix3x3{
		m[0], m[3], m[6],
		m[1], m[4], m[7],
		m[2], m[5], m[8],
	}
}

// 返回 3x3 单位矩阵
func Identity3x3() Matrix3x3 {
	return Matrix3x3{
		1, 0, 0,
		0, 1, 0,
		0, 0, 1,
	}
}

// 从向量创建对角矩阵
func Diagonal3x3(v Vector3) Matrix3x3 {
	return Matrix3x3{
		v[0], 0, 0,
		0, v[1], 0,
		0, 0, v[2],
	}
}

// Scale 缩放矩阵的所有元素
func (m Matrix3x3) Scale(s float64) Matrix3x3 {
	var result Matrix3x3
	for i := 0; i < 9; i++ {
		result[i] = m[i] * s
	}
	return result
}

// Scale3 缩放向量
func (v Vector3) Scale(s float64) Vector3 {
	return Vector3{v[0] * s, v[1] * s, v[2] * s}
}

// Add3 向量加法
func (v Vector3) Add(other Vector3) Vector3 {
	return Vector3{v[0] + other[0], v[1] + other[1], v[2] + other[2]}
}

// 逐分量乘法
func (v Vector3) ComponentMul(other Vector3) Vector3 {
	return Vector3{v[0] * other[0], v[1] * other[1], v[2] * other[2]}
}

// 逐分量求倒数
func (v Vector3) Invert() Vector3 {
	return Vector3{
		1.0 / v[0],
		1.0 / v[1],
		1.0 / v[2],
	}
}

// 将向量各分量限制在 [min, max] 范围内
func (v Vector3) Clamp(min, max float64) Vector3 {
	result := v
	for i := range result {
		if result[i] < min {
			result[i] = min
		} else if result[i] > max {
			result[i] = max
		}
	}
	return result
}
