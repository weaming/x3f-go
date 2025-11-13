#!/usr/bin/env fish

# 生成 C 版本和 Go 版本的 PPM-ASCII 输出（未裁切，已做预处理）

set X3F_FILE $argv[1]

# 检查输入文件是否存在
if not test -f "$X3F_FILE"
    echo "❌ 错误: 文件不存在: $X3F_FILE"
    exit 1
end

set C_EXTRACT ../bin/c-osx-universal/x3f_extract
set C_OUTPUT /tmp/c_preprocessed.ppm
set GO_OUTPUT /tmp/go_preprocessed.ppm

echo "生成 PPM 文件用于对比"
echo ""

# C 版本：-ppm-ascii -no-crop -color none
echo "C 版本: -ppm-ascii -no-crop -color none"
$C_EXTRACT -ppm-ascii -no-crop -color none -o /tmp $X3F_FILE 2>&1 | grep -v "^   :"
mv /tmp/(basename $X3F_FILE).ppm $C_OUTPUT
echo "  → $C_OUTPUT"

# Go 版本：-o c-osx-universal.ppm -no-crop
echo ""
echo "Go 版本: -o c-osx-universal.ppm -no-crop"
go build -o /tmp/x3f-go ./cmd/x3f-go 2>&1
/tmp/x3f-go -o $GO_OUTPUT -no-crop $X3F_FILE
echo "  → $GO_OUTPUT"
