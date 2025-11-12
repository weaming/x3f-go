#!/usr/bin/env fish

set input_file $argv[1]

# 检查输入文件是否存在
if not test -f "$input_file"
    echo "❌ 错误: 文件不存在: $input_file"
    exit 1
end

set name (basename $input_file .x3f)
set name (basename $name .X3F)

set output_dir "$HOME/Downloads"
set c_output "$output_dir/$name.go_c.dng"
set go_output "$output_dir/$name.go_nice.dng"

fish build.fish
set go_bin "./build/x3f-go"

if not test -f $go_bin
    echo "❌ 错误: 找不到可执行文件"
    exit 1
end

$go_bin -c -o $c_output $input_file
echo "c 兼容版本 dng: $c_output"
$go_bin -o $go_output $input_file
echo "  优化版本 dng: $go_output (相当于c版本的 -dng -linear-srgb)"