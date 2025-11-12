#!/usr/bin/env fish
# X3F to DNG 转换对比脚本
# 使用方法: ./compare_dng.fish <x3f文件路径> [输出目录]

set input_file $argv[1]

# 检查输入文件是否存在
if not test -f "$input_file"
    echo "❌ 错误: 文件不存在: $input_file"
    exit 1
end

# 获取输出目录 (默认为当前目录下的 dng_output)
set output_dir "$HOME/Downloads/x3f-go"

# 创建输出目录
mkdir -p $output_dir
if test $status -ne 0
    echo "❌ 错误: 无法创建输出目录: $output_dir"
    exit 1
end

# 获取文件名（不含扩展名）
set name (basename $input_file .x3f)
set name (basename $name .X3F)

# 设置输出文件路径
set c_output "$output_dir/$name"_c.dng
set go_output "$output_dir/$name"_go.dng
set c_exif "$output_dir/$name"_c_exif.txt
set go_exif "$output_dir/$name"_go_exif.txt

echo "==============================================="
echo "X3F to DNG 转换对比工具"
echo "==============================================="
echo ""
echo "输入文件: $input_file"
echo "输出目录: $output_dir"
echo ""

# ========================================
# 1. 生成 C 版本 DNG
# ========================================
echo "📦 步骤 1/5: 使用 C 版本生成 DNG..."
set c_bin "../bin/osx-universal/x3f_extract"

if not test -f $c_bin
    set c_bin "../bin/osx-arm64/x3f_extract"
    if not test -f $c_bin
        set c_bin "./bin/osx-universal/x3f_extract"
        if not test -f $c_bin
            echo "❌ 错误: 找不到 C 版本可执行文件"
            exit 1
        end
    end
end

# C 版本的 -o 参数只接受目录，会自动生成文件名
set c_temp_dir "$output_dir/.c_temp"
mkdir -p $c_temp_dir
$c_bin -dng -o $c_temp_dir $input_file
if test $status -ne 0
    echo "❌ 错误: C 版本生成失败"
    rm -rf $c_temp_dir
    exit 1
end

# 找到生成的 DNG 文件并重命名
set c_generated (find $c_temp_dir -name "*.dng" -type f | head -1)
if test -z "$c_generated"
    echo "❌ 错误: C 版本未生成 DNG 文件"
    rm -rf $c_temp_dir
    exit 1
end

mv -f $c_generated $c_output
rm -rf $c_temp_dir
echo "✓ C 版本生成成功: $c_output"
echo ""

echo "📦 步骤 2/5: 使用 Go 版本生成 DNG..."
set go_bin "./build/x3f-go"

if not test -f $go_bin
    echo "⚠️  警告: 找不到 Go 版本可执行文件，正在编译..."
    fish build.fish
    if test $status -ne 0
        echo "❌ 错误: Go 版本编译失败"
        exit 1
    end
end

$go_bin -c -o $go_output $input_file
if test $status -ne 0
    echo "❌ 错误: Go 版本生成失败"
    exit 1
end
echo "✓ Go 版本生成成功: $go_output"
echo ""

# ========================================
# 3. 提取 EXIF 信息
# ========================================
echo "📋 步骤 3/5: 提取 EXIF 元数据..."

if not command -v exiftool &>/dev/null
    echo "⚠️  警告: 未安装 exiftool，跳过 EXIF 提取"
    echo "   安装: brew install exiftool"
    set has_exiftool no
else
    exiftool -a -G1 $c_output >$c_exif 2>&1
    exiftool -a -G1 $go_output >$go_exif 2>&1
    echo "✓ EXIF 信息已保存"
    set has_exiftool yes
end
echo ""

echo "📊 步骤 4/5: 生成对比报告..."

# 获取文件大小
set c_size (stat -f%z $c_output 2>/dev/null; or stat -c%s $c_output 2>/dev/null)
set go_size (stat -f%z $go_output 2>/dev/null; or stat -c%s $go_output 2>/dev/null)
set size_diff (math "$c_size - $go_size")
set size_diff_kb (math "$size_diff / 1024")
set size_diff_mb (math "$c_size / 1048576")
set go_size_mb (math "$go_size / 1048576")
set similarity (math "100 - ($size_diff / $c_size * 100)")


echo "📊 步骤 5/5: 对比摘要"
echo "==============================================="
echo ""
printf "文件大小:\n"
printf "  C 版本:  %d 字节 (%.1f MB)\n" $c_size $size_diff_mb
printf "  Go 版本: %d 字节 (%.1f MB)\n" $go_size $go_size_mb
printf "  差异:    %d 字节 (%.1f KB)\n" $size_diff $size_diff_kb
printf "  相似度:  %.2f%%\n" $similarity
echo ""

echo "==============================================="
echo "✅ 对比完成！"
echo ""
echo "生成的文件:"
echo "  1. C 版本 DNG:      $c_output"
echo "  2. Go 版本 DNG:     $go_output"
if test "$has_exiftool" = "yes"
    echo "  3. C 版本 EXIF:     $c_exif"
    echo "  4. Go 版本 EXIF:    $go_exif"
end
echo "==============================================="

git diff --no-index $c_exif $go_exif