package main

import (
	"fmt"
	"math"
	"os"

	"github.com/weaming/x3f-go/x3f"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: test-camf <x3f文件>")
		os.Exit(1)
	}

	x3fPath := os.Args[1]

	// 打开 X3F 文件
	x3fFile, err := x3f.Open(x3fPath)
	if err != nil {
		fmt.Printf("错误: 无法打开 X3F 文件: %v\n", err)
		os.Exit(1)
	}
	defer x3fFile.Close()

	// 加载 CAMF 段
	if err := x3fFile.LoadSection(x3f.SECc); err != nil {
		fmt.Printf("错误: 无法加载 CAMF 段: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=== X3F CAMF 数据读取测试 ===")
	fmt.Printf("文件: %s\n", x3fPath)
	fmt.Printf("版本: 0x%08x\n", x3fFile.Header.Version)

	// 检查 CAMF 是否加载
	if x3fFile.CAMFSection == nil {
		fmt.Println("错误: CAMF 段未加载")
		os.Exit(1)
	}
	fmt.Printf("CAMF 段已加载，包含 %d 个条目\n", len(x3fFile.CAMFSection.Entries))
	fmt.Println()

	// 1. 测试白平衡名称
	wb := x3fFile.GetWhiteBalance()
	fmt.Printf("1. 白平衡预设: %s\n", wb)
	fmt.Println()

	// 2. 测试 ImageDepth
	if imageDepth, ok := x3fFile.GetCAMFUint32("ImageDepth"); ok {
		fmt.Printf("2. ImageDepth: %d\n", imageDepth)
		maxVal := (uint32(1) << imageDepth) - 1
		fmt.Printf("   计算的 max_raw: %d\n", maxVal)
	} else {
		fmt.Println("2. ImageDepth: 未找到")
	}
	fmt.Println()

	// 3. 测试 SaturationLevel / RawSaturationLevel
	isTRUE := x3fFile.IsTRUEEngine()
	fmt.Printf("3. 是否为 TRUE 引擎: %v\n", isTRUE)

	fieldName := "SaturationLevel"
	if isTRUE {
		fieldName = "RawSaturationLevel"
	}

	if satLevel, ok := x3fFile.GetCAMFInt32Vector(fieldName, 3); ok && len(satLevel) == 3 {
		fmt.Printf("   %s: [%d, %d, %d]\n", fieldName, satLevel[0], satLevel[1], satLevel[2])
	} else {
		fmt.Printf("   %s: 未找到\n", fieldName)
	}

	// 使用 GetMaxRaw 函数
	if maxRaw, ok := x3fFile.GetMaxRaw(); ok {
		fmt.Printf("   GetMaxRaw(): [%d, %d, %d]\n", maxRaw[0], maxRaw[1], maxRaw[2])
	}
	fmt.Println()

	// 4. 测试白平衡增益
	fmt.Printf("4. 白平衡增益 (wb=%s):\n", wb)

	// 尝试读取 WhiteBalanceGains
	if gainVec, ok := x3fFile.GetCAMFMatrixForWB("WhiteBalanceGains", wb, []uint32{3}); ok && len(gainVec) == 3 {
		fmt.Printf("   WhiteBalanceGains (直接): [%.6f, %.6f, %.6f]\n", gainVec[0], gainVec[1], gainVec[2])
	} else if gainVec, ok := x3fFile.GetCAMFMatrixForWB("DP1_WhiteBalanceGains", wb, []uint32{3}); ok && len(gainVec) == 3 {
		fmt.Printf("   DP1_WhiteBalanceGains (直接): [%.6f, %.6f, %.6f]\n", gainVec[0], gainVec[1], gainVec[2])
	} else {
		fmt.Println("   WhiteBalanceGains: 未找到（将使用 Illuminants 计算）")
	}

	// 测试校正系数
	if sensorGain, ok := x3fFile.GetCAMFFloatVector("SensorAdjustmentGainFact", 3); ok && len(sensorGain) == 3 {
		fmt.Printf("   SensorAdjustmentGainFact: [%.6f, %.6f, %.6f]\n", sensorGain[0], sensorGain[1], sensorGain[2])
	} else {
		fmt.Println("   SensorAdjustmentGainFact: 未找到")
	}

	if tempGain, ok := x3fFile.GetCAMFFloatVector("TempGainFact", 3); ok && len(tempGain) == 3 {
		fmt.Printf("   TempGainFact: [%.6f, %.6f, %.6f]\n", tempGain[0], tempGain[1], tempGain[2])
	} else {
		fmt.Println("   TempGainFact: 未找到")
	}

	if fNumberGain, ok := x3fFile.GetCAMFFloatVector("FNumberGainFact", 3); ok && len(fNumberGain) == 3 {
		fmt.Printf("   FNumberGainFact: [%.6f, %.6f, %.6f]\n", fNumberGain[0], fNumberGain[1], fNumberGain[2])
	} else {
		fmt.Println("   FNumberGainFact: 未找到")
	}

	// 使用 GetWhiteBalanceGain 函数（包含所有校正）
	if gain, ok := x3fFile.GetWhiteBalanceGain(wb); ok {
		fmt.Printf("   GetWhiteBalanceGain() 最终增益: [%.6f, %.6f, %.6f]\n", gain[0], gain[1], gain[2])
	} else {
		fmt.Println("   GetWhiteBalanceGain(): 失败")
	}
	fmt.Println()

	// 5. 测试色彩矩阵
	fmt.Printf("5. 色彩矩阵 (wb=%s):\n", wb)

	// 先查看原始的 ColorCorrections 矩阵
	if ccMatrix, ok := x3fFile.GetCAMFMatrixForWB("WhiteBalanceColorCorrections", wb, []uint32{3, 3}); ok && len(ccMatrix) == 9 {
		fmt.Println("   WhiteBalanceColorCorrections (原始):")
		fmt.Printf("   [%.6f, %.6f, %.6f]\n", ccMatrix[0], ccMatrix[1], ccMatrix[2])
		fmt.Printf("   [%.6f, %.6f, %.6f]\n", ccMatrix[3], ccMatrix[4], ccMatrix[5])
		fmt.Printf("   [%.6f, %.6f, %.6f]\n", ccMatrix[6], ccMatrix[7], ccMatrix[8])
	}

	if bmtToXYZ, ok := x3fFile.GetBMTToXYZ(wb); ok && len(bmtToXYZ) == 9 {
		fmt.Println("   BMT -> XYZ 矩阵:")
		fmt.Printf("   [%.6f, %.6f, %.6f]\n", bmtToXYZ[0], bmtToXYZ[1], bmtToXYZ[2])
		fmt.Printf("   [%.6f, %.6f, %.6f]\n", bmtToXYZ[3], bmtToXYZ[4], bmtToXYZ[5])
		fmt.Printf("   [%.6f, %.6f, %.6f]\n", bmtToXYZ[6], bmtToXYZ[7], bmtToXYZ[8])

		// 计算 ColorMatrix1 (XYZ -> BMT)
		xyzToBMT := x3f.Inverse3x3(bmtToXYZ)
		fmt.Println("   ColorMatrix1 (XYZ -> BMT):")
		fmt.Printf("   [%.6f, %.6f, %.6f]\n", xyzToBMT[0], xyzToBMT[1], xyzToBMT[2])
		fmt.Printf("   [%.6f, %.6f, %.6f]\n", xyzToBMT[3], xyzToBMT[4], xyzToBMT[5])
		fmt.Printf("   [%.6f, %.6f, %.6f]\n", xyzToBMT[6], xyzToBMT[7], xyzToBMT[8])
	} else {
		fmt.Println("   GetBMTToXYZ(): 失败")
	}
	fmt.Println()

	// 6. 测试黑电平计算所需数据
	fmt.Println("6. 黑电平相关:")

	if gain, ok := x3fFile.GetWhiteBalanceGain(wb); ok {
		maxRaw, _ := x3fFile.GetMaxRaw()

		// 模拟 C 代码的 intermediate_bias 计算
		// 使用默认黑电平值
		blackLevel := x3f.DefaultBlackLevel
		blackDev := x3f.DefaultBlackDev

		fmt.Printf("   BlackLevel (默认): [%.6f, %.6f, %.6f]\n", blackLevel[0], blackLevel[1], blackLevel[2])
		fmt.Printf("   BlackDev (默认): [%.6f, %.6f, %.6f]\n", blackDev[0], blackDev[1], blackDev[2])

		// 计算 max_intermediate (bias=0)
		maxGain := gain[0]
		if gain[1] > maxGain {
			maxGain = gain[1]
		}
		if gain[2] > maxGain {
			maxGain = gain[2]
		}

		maxIntermediate := [3]uint32{}
		for i := 0; i < 3; i++ {
			maxIntermediate[i] = uint32(gain[i] * float64(x3f.INTERMEDIATE_UNIT) / maxGain)
		}
		fmt.Printf("   MaxIntermediate (bias=0): [%d, %d, %d]\n", maxIntermediate[0], maxIntermediate[1], maxIntermediate[2])

		// 计算 intermediate_bias
		intermediateBias := 0.0
		for i := 0; i < 3; i++ {
			bias := x3f.INTERMEDIATE_BIAS_FACTOR * blackDev[i] *
				float64(maxIntermediate[i]) / (float64(maxRaw[i]) - blackLevel[i])
			if bias > intermediateBias {
				intermediateBias = bias
			}
		}
		fmt.Printf("   IntermediateBias (计算): %.6f\n", intermediateBias)

		// 计算最终 WhiteLevel
		whiteLevel := [3]uint32{}
		fmt.Println("   WhiteLevel 计算详情:")
		for i := 0; i < 3; i++ {
			value := gain[i]*(float64(x3f.INTERMEDIATE_UNIT)-intermediateBias)/maxGain + intermediateBias
			whiteLevel[i] = uint32(math.Round(value))
			fmt.Printf("     通道[%d]: %.6f → %d\n", i, value, whiteLevel[i])
		}
		fmt.Printf("   WhiteLevel (最终): [%d, %d, %d]\n", whiteLevel[0], whiteLevel[1], whiteLevel[2])
	}
	fmt.Println()

	fmt.Println("=== 测试完成 ===")
}
