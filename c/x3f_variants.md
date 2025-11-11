# 不同相机的解析差别
## 给人看的简明说明：为什么解析 X3F 这么难？

将 X3F 文件想象成一个高安全性的公文包 (`.x3f` 文件)。无论哪个相机（型号）制造了这个公文包，它的**外部结构**总是相同的：

1.  **公文包本身：** 总是以 "FOVb" 标识开头。
2.  **锁（目录指针）：** 锁的位置总是在公文包的**最末端**（最后 4 个字节）。
3.  **内容索引（目录）：** 这把锁（指针）指向一个“内容索引”（`SECd` 节），它告诉您公文包里有哪些文件（`PROP` 属性、`IMAG` 图像、`CAMF` 相机数据）。

**真正的区别在于“公文包里的文件”**。不同代的相机使用不同的**加密方式**和**压缩算法**来存储这些内部文件。`x3f_io.c` 的工作就是充当一个“万能钥匙”匠，它必须在打开公文包后，根据里面的文件类型来选择正确的工具。

### 主要区别 1：图像数据 (IMAG 节)

这是最明显的区别。代码在 `x3f_load_image` (L1285) 函数中查看图像的 `type_format` 字段：

*   **老式相机 (如 SD15, DP1/DP2)：** 图像被 `X3F_IMAGE_RAW_HUFFMAN_...` (L1304) 压缩。代码必须调用**霍夫曼 (Huffman) 解码器** (`huffman_decode` L925) 来重建图像。
*   **Merrill 和 Quattro 相机：** 图像被 `X3F_IMAGE_RAW_TRUE` 或 `_QUATTRO` (L1298) 压缩。代码必须调用一个完全不同的、更复杂的**“TRUE”解码器** (`true_decode` L1206)。
*   **Quattro 的特殊情况：** Quattro 相机 还有一个特殊分支。代码必须检查 `Q->quattro_layout` (L1169)，因为这种传感器的顶层（蓝色）分辨率是底层（红/绿）的 4 倍。如果这个标志为真，解码器必须使用*不同*的内存布局和循环参数 (`true_decode_one_color` L787) 来正确重建图像。

### 主要区别 2：加密的元数据 (CAMF 节)

这是最隐秘的区别。CAMF 节 包含了相机传感器校准等关键数据。**几乎每一代相机都使用了不同的加密算法**。

`x3f_load_camf` (L1532) 函数会检查 `CAMF->type` 字段 (L1539)：

*   **Type 2 (老式 SD 相机)：** 代码调用 `x3f_load_camf_decode_type2` (L1328)。这是一个流密码，使用一个简单的数学公式 (`key = (key * 1597 + 51749) % 244944`) 和 XOR 运算来解密数据。
*   **Type 4 (Merrill 相机)：** 代码调用 `x3f_load_camf_decode_type4` (L1473)。这是一个完全不同的、基于霍夫曼的解混淆算法。
*   **Type 5 (Quattro 相机)：** 代码调用 `x3f_load_camf_decode_type5` (L1506)。这是*又一种*基于霍夫曼的解混淆算法。

**结论：**
一个 X3F 解析器**不能**“一招鲜吃遍天”。它必须在读取文件的“索引”后，通过检查 `IMAG` 和 `CAMF` 节中的 `type` 字段，来**分支**其逻辑，为特定相机（或相机世代）调用正确的解压缩和解密程序。

---

## 给 AI/开发者的实现说明：关键分支点

要实现对 `x3f_io.c` 所支持的 X3F 文件的完全覆盖，必须正确实现以下基于相机型号（或文件版本）的关键分支点。这些分支决定了调用哪个解码或解密例程。

| 目标 | 关键字段 (来自 `x3f_io.c`) | 代码分支逻辑 (在 `x3f_io.c` 中) | 目的与区别 |
| :--- | :--- | :--- | :--- |
| **文件头解析** | `H->version` | `if (H->version < X3F_VERSION_4_0)` (L271) | `v4.0` (Quattro) 之前的头部有 `extended_data` 字段；v4.0+ 的头部结构不同（或未知）。 |
| **图像解码** | `ID->type_format` | `switch (ID->type_format)` (L1292) (在 `x3f_load_image` 中) | **主要的图像解码器分发器**。 |
| older | `X3F_IMAGE_RAW_HUFFMAN_...` (L1304) | `x3f_load_huffman(...)` (L1228) | (SD15, DP1/2) 调用霍夫曼 DPCM 解码器。注意 `auto_legacy_offset` (L928) 逻辑，用于处理传感器黑电平不一致（尤其是 Merrill）。 |
| Merrill | `X3F_IMAGE_RAW_TRUE` / `_MERRILL` (L1298) | `x3f_load_true(...)` (L1154) | (Merrill) 调用 TRUE DPCM 解码器。 |
| Quattro | `X3F_IMAGE_RAW_QUATTRO` / `_SDQ` / `_SDQH` (L1300) | `x3f_load_true(...)` (L1154) | (Quattro) **复用** `x3f_load_true`，但依赖于下面的子分支。 |
| **图像解码 (Quattro 子分支)** | `Q->quattro_layout` | `if (Q->quattro_layout)` (L1169) (在 `x3f_load_true` 中) | **关键！** 此布尔值区分了“Binned Quattro”（`false`，1:1:1 布局）和“Quattro”（`true`，4:1:1 布局）。 |
| | | `true_decode_one_color` (L787) | 如果 `Q->quattro_layout` 为 `true`，`color == 2`（顶层）时，`area` 指针会指向 `Q->top16`，并且循环边界 (`rows`/`cols`) 使用不同的值。 |
| **元数据解密** | `CAMF->type` | `switch (CAMF->type)` (L1539) (在 `x3f_load_camf` 中) | **主要的解密器分发器**。 |
| older | `case 2:` (L1541) | `x3f_load_camf_decode_type2` (L1328) | (SD9-SD14) 实现 LCG 流密码 + XOR 字节解密。 |
| Merrill | `case 4:` (L1544) | `x3f_load_camf_decode_type4` (L1473) | (Merrill) 实现基于霍夫曼的解混淆（类似于 `true_decode`）。 |
| Quattro | `case 5:` (L1547) | `x3f_load_camf_decode_type5` (L1506) | (Quattro) 实现另一种基于霍夫曼的解混淆（类似于 `true_decode`，但具有不同的打包逻辑）。 |