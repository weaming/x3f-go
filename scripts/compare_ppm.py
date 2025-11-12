#!/usr/bin/env python3
"""
比较两个 PPM 文件并生成统计信息
"""

import sys
from typing import Tuple, List

def compare_ppm_files(file1: str, file2: str, sample_lines: int = 5) -> dict:
    """
    比较两个 PPM 文件

    Args:
        file1: 第一个 PPM 文件路径
        file2: 第二个 PPM 文件路径
        sample_lines: 显示多少个差异样本

    Returns:
        包含统计信息的字典
    """
    stats = {
        'total_lines': 0,
        'diff_lines': 0,
        'diff_rate': 0.0,
        'first_diff_line': None,
        'max_channel_diff': 0,
        'sample_diffs': []
    }

    try:
        with open(file1) as f1, open(file2) as f2:
            for line_num, (line1, line2) in enumerate(zip(f1, f2), 1):
                stats['total_lines'] += 1

                if line1 != line2:
                    stats['diff_lines'] += 1

                    # 记录第一个差异
                    if stats['first_diff_line'] is None:
                        stats['first_diff_line'] = line_num

                    # 保存差异样本
                    if len(stats['sample_diffs']) < sample_lines:
                        stats['sample_diffs'].append({
                            'line': line_num,
                            'file1': line1.strip(),
                            'file2': line2.strip()
                        })

                    # 如果是像素数据行，计算最大通道差异
                    try:
                        vals1 = list(map(int, line1.strip().split()))
                        vals2 = list(map(int, line2.strip().split()))
                        if len(vals1) == 3 and len(vals2) == 3:
                            for v1, v2 in zip(vals1, vals2):
                                diff = abs(v1 - v2)
                                stats['max_channel_diff'] = max(stats['max_channel_diff'], diff)
                    except:
                        pass

            # 检查文件长度是否相同
            remaining1 = list(f1)
            remaining2 = list(f2)
            if remaining1 or remaining2:
                print(f"⚠ 警告: 文件长度不同!")
                if remaining1:
                    print(f"  文件1 还有 {len(remaining1)} 行")
                if remaining2:
                    print(f"  文件2 还有 {len(remaining2)} 行")

        # 计算差异率
        if stats['total_lines'] > 0:
            stats['diff_rate'] = (stats['diff_lines'] / stats['total_lines']) * 100

        return stats

    except FileNotFoundError as e:
        print(f"✗ 错误: 文件不存在 - {e}")
        sys.exit(1)
    except Exception as e:
        print(f"✗ 错误: {e}")
        sys.exit(1)

def print_stats(stats: dict, file1_name: str, file2_name: str):
    """打印统计信息"""
    print(f"\n{'='*60}")
    print(f"PPM 文件比较统计")
    print(f"{'='*60}")
    print(f"文件1: {file1_name}")
    print(f"文件2: {file2_name}")
    print(f"{'-'*60}")
    print(f"总行数:         {stats['total_lines']:,}")
    print(f"不同行数:       {stats['diff_lines']:,}")
    print(f"差异率:         {stats['diff_rate']:.4f}%")
    print(f"第一个差异:     行 {stats['first_diff_line']}" if stats['first_diff_line'] else "第一个差异:     无")
    print(f"最大通道差异:   {stats['max_channel_diff']}")
    print(f"{'-'*60}")

    if stats['diff_lines'] == 0:
        print("\n✓✓✓ 完全一致! ✓✓✓")
    else:
        print(f"\n差异样本 (前 {len(stats['sample_diffs'])} 个):")
        for sample in stats['sample_diffs']:
            print(f"  行 {sample['line']:7d}:")
            print(f"    文件1: {sample['file1']}")
            print(f"    文件2: {sample['file2']}")

def analyze_pixel_position(line_num: int, width: int = 4928, height: int = 3264):
    """分析像素在图像中的位置"""
    # 减去 3 行 header
    pixel_num = line_num - 3
    if pixel_num < 0:
        return None

    row = pixel_num // width
    col = pixel_num % width
    progress = (row / height) * 100 if height > 0 else 0

    return {
        'pixel_num': pixel_num,
        'row': row,
        'col': col,
        'progress': progress
    }

def main():
    if len(sys.argv) < 3:
        print("用法: compare_ppm.py <file1.ppm> <file2.ppm> [sample_lines]")
        print("示例: compare_ppm.py /tmp/c.ppm /tmp/go.ppm 10")
        sys.exit(1)

    file1 = sys.argv[1]
    file2 = sys.argv[2]
    sample_lines = int(sys.argv[3]) if len(sys.argv) > 3 else 5

    # 比较文件
    stats = compare_ppm_files(file1, file2, sample_lines)

    # 打印统计
    print_stats(stats, file1, file2)

    # 如果有差异，分析第一个差异的位置
    if stats['first_diff_line']:
        pos = analyze_pixel_position(stats['first_diff_line'])
        if pos:
            print(f"\n第一个差异的位置:")
            print(f"  像素编号: {pos['pixel_num']}")
            print(f"  图像坐标: 行 {pos['row']}, 列 {pos['col']}")
            print(f"  进度: {pos['progress']:.2f}%")

if __name__ == '__main__':
    main()
