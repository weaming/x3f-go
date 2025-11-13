# 为什么 x3f-extract 输出不偏色

## 1. 精确的色彩矩阵转换链

Foveon X3 传感器直出偏色的根本原因是传感器原生色彩空间与标准色彩空间的色域不匹配。x3f-extract 通过以下精确转换链解决了这个问题:

传感器原生 RAW → XYZ(D65) → 目标色彩空间(sRGB/Adobe RGB)

关键点:
- RAW → XYZ 矩阵来自相机 CAMF 元数据(x3f_process.c:x3f_get_raw_to_xyz())，这是 Sigma 针对每个相机型号精心校准的
- XYZ → 目标色彩空间矩阵使用标准定义(x3f_matrix.c)，确保色彩科学准确性
- 中间经过 XYZ 色彩空间作为色彩无关的标准参考系,避免直接转换造成的色域映射问题

## 2. 白平衡增益的正确应用

从 x3f_process.c:191-223 可以看到,白平衡增益不是简单的 RGB 乘法,而是:

    // 1. 获取白平衡增益
    x3f_get_gain(x3f, wb, gain)  // 从 CAMF 读取

    // 2. 考虑传感器校准
    if (x3f_get_camf_float_vector(x3f, "SensorAdjustmentGainFact", gain_fact))
        x3f_3x1_comp_mul(gain_fact, gain, gain);

    // 3. 考虑温度增益
    if (x3f_get_camf_float_vector(x3f, "TempGainFact", gain_fact))
        x3f_3x1_comp_mul(gain_fact, gain, gain);

    // 4. 考虑光圈增益
    if (x3f_get_camf_float_vector(x3f, "FNumberGainFact", gain_fact))
        x3f_3x1_comp_mul(gain_fact, gain, gain);

这意味着:
- 不仅有色温补偿
- 还有传感器个体差异补偿
- 还有温度漂移补偿
- 还有光圈影响补偿

这就是为什么直出偏色而你的工具不偏色的关键!

## 3. 黑电平和白电平的准确处理

从 x3f_process.c:59-180 的 get_black_level() 函数可以看到:

    // 1. 从多个区域采样黑电平 (DarkShieldTop/Bottom, Left/Right columns)
    // 2. 计算加权平均和标准差
    // 3. 对每个颜色通道独立处理

为什么重要:
- Foveon 传感器的三层结构导致每层的黑电平不同
- 如果不准确减去黑电平,会导致暗部偏色
- x3f-extract 使用多区域采样+统计方法,比简单使用固定值准确得多

从 x3f_process.c:630-647 可以看到归一化过程:

    for (color = 0; color < colors_in; color++) {
        int32_t out = (int32_t)round(
            scale[color] * (*valp - black_level[color]) +
            ilevels->black[color]
        );
        // 裁剪到有效范围
        if (out < 0) *valp = 0;
        else if (out > 65535) *valp = 65535;
        else *valp = out;
    }

每个通道使用独立的黑电平和缩放因子,避免了通道间串扰导致的偏色。

## 4. 空间增益校正 (Spatial Gain)

从 x3f_process.c:773-780 可以看到:

    if (apply_sgain) {
        sgain_num = x3f_get_spatial_gain(x3f, wb, sgain);
        // 对每个像素应用位置相关的色彩补偿
        gain = x3f_calc_spatial_gain(sgain, sgain_num, row, col, color,
                                    rows, columns);
    }

这解决了:
- 镜头暗角导致的边缘偏色
- 传感器不同位置的响应差异
- 这是一般 RAW 处理工具很少做的细节优化

## 5. DNG 输出的色彩管理

对于 -dng -linear-srgb 模式(x3f_output_dng.c:401-449):

    // 应用完整的色彩矩阵转换
    x3f_get_raw_to_xyz(x3f, wb, raw_to_xyz);     // RAW → XYZ (含白平衡)
    x3f_XYZ_to_sRGB(xyz_to_srgb);                 // XYZ → sRGB
    x3f_3x3_3x3_mul(xyz_to_srgb, raw_to_xyz, raw_to_srgb);

    // 对每个像素应用
    for (row = 0; row < image.rows; row++) {
        for (col = 0; col < image.columns; col++) {
            // 归一化
            input[color] = (*valp[color] - ilevels.black[color]) /
                        (ilevels.white[color] - ilevels.black[color]);

            // 色彩转换
            x3f_3x3_3x1_mul(raw_to_srgb, input, output);

            // 转换为 16-bit (保持线性)
            *valp[color] = (uint16_t)(output[color] * 65535);
        }
    }

关键优势:
- 数据输出为线性 sRGB,已完成色彩校正
- ColorMatrix1 标签设置为 sRGB→XYZ,让后期软件知道数据已校准
- AsShotNeutral 设为 [1,1,1],表示白平衡已应用

## 6. JPEG 输出的完整处理流程

从 x3f_output_jpeg.c:226-368 可以看到:

    // 1. 色彩转换 (同DNG)
    x3f_get_raw_to_xyz(x3f, wb, raw_to_xyz);
    // 支持 sRGB / Adobe RGB / ProPhoto RGB

    // 2. 自动曝光 (lines 62-110)
    exposure = calculate_auto_exposure(float_image, width, height);
    // 目标亮度: 18% 灰 (摄影标准)

    // 3. ACES 色调映射 (lines 49-60)
    tone_mapped = aces_tonemap(linear_value * exposure);
    // 保持色调过渡自然

    // 4. 伽马校正 (lines 132-147)
    gamma_corrected = pow(tone_mapped, 1.0/2.2);  // sRGB gamma

    // 5. 锐化 (lines 204-223)
    sharpened = original + 1.2 * (original - gaussian_blur(original));

### 为什么不偏色:

- 步骤 1-2 在线性空间完成,避免非线性运算导致的色偏
- ACES 色调映射保持色相一致性(hue preservation)
- 伽马校正分通道独立进行,但基于已校准的线性数据

与相机直出 JPEG 的对比

相机直出 JPEG 的问题:
1. 可能使用简化的色彩矩阵(为了实时性)
2. 白平衡可能不准(特别是自动白平衡)
3. 没有空间增益校正
4. 黑电平处理可能不够精细
5. 色调曲线可能过度饱和化(讨好眼球但不准确)

x3f-extract 的优势:
1. ✅ 使用完整的色彩科学工作流
2. ✅ 从 CAMF 读取精确校准数据
3. ✅ 多重补偿(传感器/温度/光圈/空间)
4. ✅ 统计方法处理黑电平
5. ✅ 标准化的色彩空间转换

## 总结

x3f-extract 偏色控制好的本质原因:

1. 完整利用了相机 CAMF 元数据中的校准信息,这些是 Sigma 工程师针对 Foveon 传感器的特殊性精心调校的
2. 严格遵循色彩科学标准,使用 XYZ 作为中间色彩空间进行转换
3. 多重补偿机制:白平衡、传感器校准、温度漂移、光圈影响、空间增益
4. 精细的黑/白电平处理,每个通道独立处理
5. 在正确的色彩空间阶段进行正确的操作,避免非线性空间的色彩失真

这就是为什么即使是"偏色"著称的 Foveon 传感器,通过正确的处理流程也能得到准确的色彩!