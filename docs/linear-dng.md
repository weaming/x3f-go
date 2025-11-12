https://www.kronometric.org/phot/processing/DNG/Linear%20DNG.htm

**1. 什么是 Linear DNG？**
* **定义：** Linear DNG 是一种存储经过“去马赛克”（demosaiced）处理后的 RGB 图像数据的 DNG 格式。
* **与标准 Raw DNG 的区别：**
    * **Raw DNG：** 包含原始的传感器数据（未去马赛克），是目前最常见的 DNG 形式。
    * **Linear DNG：** 数据已经变成了线性的 RGB 数据（类似 TIFF），但它仍然保留在“场景相关”（scene-referred）的线性空间中，没有进行白平衡、色调映射或色彩空间转换。
* **来源：** 它可以来自相机的 Raw 文件（经过转换），也可以来自扫描仪、TIFF 甚至 JPEG 文件。

**2. Linear DNG 的主要优势**
* **支持特殊的传感器结构：** 对于使用非拜耳（non-Bayer）阵列传感器的相机（如 Foveon X3 传感器），如果通用的 Raw 转换器无法直接读取其原始 Raw 数据，可以先将其转换为 Linear DNG。这样，通用的转换器（如 Lightroom 或 ACR）就能处理它了，因为复杂的去马赛克工作已经完成了。
* **开启新型图像处理流程：** 即使图像不是来自 Raw（例如来自 TIFF 或 JPEG），转换为 Linear DNG 后，依然可以使用 Raw 转换器（如 Adobe Camera Raw）的强大功能（如白平衡调整、色差校正、降噪等）进行非破坏性编辑。这模糊了“Raw 转换器”和“图像编辑器”的界限。
* **老软件使用新算法：** 如果你的软件版本较老（例如旧版 Photoshop），不支持新相机的 Raw 格式，你可以用最新的免费 DNG Converter 将新相机的 Raw 转为 Linear DNG。这样，老软件就能读取并处理这些图像，同时享受到转换器中最新的去马赛克算法带来的画质提升。

**3. 缺点**
* **不可逆的转换：** 一旦转换为 Linear DNG，去马赛克过程就已经固定了（"baked in"）。这意味着你以后无法利用其他软件可能更优秀的去马赛克算法重新处理这张图。
* **文件体积：** Linear DNG 文件通常比原始的 Raw DNG 文件要大。

**4. 支持的软件**
* **Adobe 系列：** Photoshop, Lightroom, DNG Converter 等均支持读取和写入 Linear DNG。
* **其他软件：** DxO Optics Pro（可输出 Linear DNG）、Silkypix、LightZone、dcraw 等也支持读取或处理这种格式。

**总结：**
Linear DNG 是一种介于“纯 Raw”和“成品 TIFF/JPEG”之间的中间格式。它牺牲了后期重新进行去马赛克的可能性，换取了极佳的兼容性，让不支持特定相机 Raw 格式的软件也能像处理 Raw 一样处理图像。