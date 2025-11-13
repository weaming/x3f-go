#!/usr/bin/env python3
"""
对比 C 和 Go 版本的 Quattro top 层输出

使用方法:
    python3 compare_qtop.py <x3f文件> [输出目录]

示例:
    python3 compare_qtop.py ../raw/DP3Q0109.X3F /tmp
"""

import os
import subprocess
import sys

import numpy as np
from PIL import Image


def run_command(cmd):
    """运行命令并返回结果"""
    result = subprocess.run(cmd, shell=True, capture_output=True, text=True)
    return result.returncode, result.stdout, result.stderr


def generate_c_qtop(x3f_file, output_dir):
    """生成 C 版本的 qtop 输出"""
    c_extract = "../bin/c-osx-universal/x3f_extract"

    if not os.path.exists(c_extract):
        print(f"错误: C 提取工具不存在: {c_extract}")
        return None

    basename = os.path.basename(x3f_file)
    tiff_path = os.path.join(output_dir, f"{basename}.tif")

    # 删除旧文件
    if os.path.exists(tiff_path):
        os.remove(tiff_path)

    cmd = f"{c_extract} -tiff -qtop -no-crop -o {output_dir} {x3f_file}"
    print(f"运行 C 版本: {cmd}")

    returncode, stdout, stderr = run_command(cmd)

    if returncode != 0:
        print(f"C 版本执行失败: {stderr}")
        return None

    if not os.path.exists(tiff_path):
        print(f"错误: C 版本未生成输出文件: {tiff_path}")
        return None

    return tiff_path


def generate_go_qtop(x3f_file, output_dir):
    """生成 Go 版本的 qtop 输出"""
    go_binary = "/tmp/x3f-go"

    # 编译 Go 版本
    print("编译 Go 版本...")
    compile_cmd = "go build -o /tmp/x3f-go ./cmd/x3f-go"
    returncode, _, stderr = run_command(compile_cmd)

    if returncode != 0:
        print(f"Go 编译失败: {stderr}")
        return None

    ppm_path = os.path.join(output_dir, "go_qtop.ppm")

    # 删除旧文件
    if os.path.exists(ppm_path):
        os.remove(ppm_path)

    cmd = f"{go_binary} -qtop -no-crop -o {ppm_path} {x3f_file}"
    print(f"运行 Go 版本: {cmd}")

    returncode, stdout, stderr = run_command(cmd)

    if returncode != 0:
        print(f"Go 版本执行失败: {stderr}")
        return None

    if not os.path.exists(ppm_path):
        print(f"错误: Go 版本未生成输出文件: {ppm_path}")
        return None

    return ppm_path


def compare_qtop_outputs(tiff_path, ppm_path):
    """对比 C 版本的 TIFF 和 Go 版本的 PPM"""
    print("\n=== 加载图像 ===")

    # 读取 TIFF
    img_tiff = Image.open(tiff_path)
    arr_tiff = np.array(img_tiff)

    print(f"C 版本 (TIFF): {arr_tiff.shape}, dtype: {arr_tiff.dtype}")

    # 读取 PPM 头部
    with open(ppm_path, 'r') as f:
        magic = f.readline().strip()
        dims = f.readline().strip().split()
        maxval = f.readline().strip()

        if magic != "P3":
            print(f"错误: PPM 格式不正确，magic={magic}")
            return False

        width, height = int(dims[0]), int(dims[1])
        print(f"Go 版本 (PPM): {height}x{width}, maxval={maxval}")

        if arr_tiff.shape != (height, width):
            print(f"错误: 尺寸不匹配")
            return False

    print("\n=== 对比像素数据 ===")
    total_pixels = arr_tiff.size
    batch_size = 10000
    mismatches = 0
    checked = 0
    mismatch_samples = []

    with open(ppm_path, 'r') as f:
        # 跳过头部
        f.readline()
        f.readline()
        f.readline()

        # 逐批对比
        for i in range(0, total_pixels, batch_size):
            batch_end = min(i + batch_size, total_pixels)
            ppm_batch = []

            for j in range(i, batch_end):
                line = f.readline().strip()
                if line:
                    vals = line.split()
                    if len(vals) >= 3:
                        ppm_batch.append(int(vals[0]))

            tiff_batch = arr_tiff.flat[i:batch_end]

            for k, (ppm_val, tiff_val) in enumerate(zip(ppm_batch, tiff_batch)):
                if ppm_val != int(tiff_val):
                    mismatches += 1
                    if len(mismatch_samples) < 10:
                        pixel_idx = i + k
                        row = pixel_idx // width
                        col = pixel_idx % width
                        mismatch_samples.append(
                            {'index': pixel_idx, 'row': row, 'col': col, 'ppm': ppm_val, 'tiff': int(tiff_val)}
                        )

            checked = batch_end
            if (i // batch_size) % 100 == 0:
                progress = 100 * checked // total_pixels
                print(f"  进度: {checked:,}/{total_pixels:,} ({progress}%)", end='\r')

    print(f"\n\n=== 对比结果 ===")
    print(f"总像素数: {total_pixels:,}")
    print(f"已检查: {checked:,}")
    print(f"不匹配: {mismatches:,}")

    if mismatches == 0:
        print("\n✓✓✓ 完美！C 和 Go 版本的 -qtop 输出完全相同！")
        return True
    else:
        print(f"\n✗ 存在 {mismatches} 个不匹配像素 ({100*mismatches/checked:.4f}%)")
        print("\n前 10 个不匹配的像素:")
        for sample in mismatch_samples[:10]:
            print(
                f"  像素 {sample['index']} (row={sample['row']}, col={sample['col']}): "
                f"PPM={sample['ppm']}, TIFF={sample['tiff']}, "
                f"差值={abs(sample['ppm']-sample['tiff'])}"
            )
        return False


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(1)

    x3f_file = sys.argv[1]
    output_dir = sys.argv[2] if len(sys.argv) > 2 else "/tmp"

    if not os.path.exists(x3f_file):
        print(f"错误: 文件不存在: {x3f_file}")
        sys.exit(1)

    print(f"对比文件: {x3f_file}")
    print(f"输出目录: {output_dir}\n")

    # 生成 C 版本输出
    tiff_path = generate_c_qtop(x3f_file, output_dir)
    if not tiff_path:
        sys.exit(1)

    # 生成 Go 版本输出
    ppm_path = generate_go_qtop(x3f_file, output_dir)
    if not ppm_path:
        sys.exit(1)

    # 对比输出
    success = compare_qtop_outputs(tiff_path, ppm_path)

    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()
