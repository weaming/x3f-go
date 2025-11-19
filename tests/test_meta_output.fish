#!/usr/bin/env fish

# X3F Go 版本 meta 输出对比测试脚本

set SCRIPT_DIR (dirname (status -f))
cd $SCRIPT_DIR

set C_EXTRACT ../bin/c-osx-universal/x3f_extract
set GO_EXTRACT ../build/x3f-go
set RAW_DIR ../../x3f_test_files # https://github.com/weaming/x3f_test_files

set test_files (ls $RAW_DIR | grep -i '\.x3f$')

echo "=== X3F Meta 输出对比测试 ==="
echo ""

set pass_count 0
set fail_count 0

function remove_tail_space -a file
    sed -i '' -E 's/[[:space:]]+$//' $file
end

for f in $test_files
    set raw_path $RAW_DIR/$f

    if not test -f $raw_path
        echo "✗ $f - 文件不存在"
        set fail_count (math $fail_count + 1)
        continue
    end

    # 清理旧文件
    rm -f $raw_path.meta

    # 生成 C 版本输出
    $C_EXTRACT -meta $raw_path >/dev/null 2>&1
    set tmp_txt /tmp/(basename $f .x3f)_c.txt
    mv $raw_path.meta $tmp_txt
    remove_tail_space $tmp_txt

    # 生成 Go 版本输出
    $GO_EXTRACT -meta $raw_path >/dev/null 2>&1
    set tmp_txt /tmp/(basename $f .x3f)_go.txt
    mv $raw_path.meta $tmp_txt
    remove_tail_space $tmp_txt


    # 对比
    if diff -q /tmp/(basename $f .x3f)_c.txt /tmp/(basename $f .x3f)_go.txt >/dev/null 2>&1
        echo "✓ $f - 完全一致"
        set pass_count (math $pass_count + 1)
    else
        echo "✗ $f - 有差异"
        set fail_count (math $fail_count + 1)
        diff /tmp/(basename $f .x3f)_c.txt /tmp/(basename $f .x3f)_go.txt | head -20
    end
end

echo ""
echo "=== 测试结果 ==="
echo "通过: $pass_count"
echo "失败: $fail_count"

# 清理临时文件
rm -f /tmp/*_c.txt /tmp/*_go.txt

if test $fail_count -eq 0
    exit 0
else
    exit 1
end
