#!/usr/bin/env python3
"""将TIFF转换为PPM格式（用于对比）"""

import sys
from PIL import Image
import numpy as np

def tiff_to_ppm(tiff_path, ppm_path):
    """将TIFF文件转换为PPM P3格式"""
    # 读取TIFF
    img = Image.open(tiff_path)
    data = np.array(img)

    height, width = data.shape

    # 写入PPM P3格式
    with open(ppm_path, 'w') as f:
        # PPM头部
        f.write(f"P3\n{width} {height}\n65535\n")

        # 写入像素数据（灰度图转为R=G=B）
        for row in data:
            for val in row:
                f.write(f"{val} {val} {val} \n")

    print(f"转换完成: {tiff_path} -> {ppm_path}")
    print(f"尺寸: {width}x{height}")

if __name__ == "__main__":
    if len(sys.argv) != 3:
        print(f"用法: {sys.argv[0]} <input.tiff> <output.ppm>")
        sys.exit(1)

    tiff_to_ppm(sys.argv[1], sys.argv[2])
