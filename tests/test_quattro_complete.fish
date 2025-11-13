#!/usr/bin/env fish

# Quattro 格式完整对比测试套件
# 包含所有已通过的测试：-unprocessed、-qtop等

set X3F_FILE "raw/DP3Q0109.X3F"

# 检查输入文件是否存在
if not test -f "$X3F_FILE"
    echo "❌ 错误: 文件不存在: $X3F_FILE"
    exit 1
end

set C_EXTRACT ../bin/c-osx-universal/x3f_extract

# 编译Go版本
echo "🔨 编译 Go 版本..."
go build -o /tmp/x3f-go ./cmd/x3f-go 2>&1
echo ""

set all_passed 1

# ============================================================================
echo "═══════════════════════════════════════════════════════════════════════"
echo "  测试1: -unprocessed -no-crop（完整未裁剪的原始数据）"
echo "═══════════════════════════════════════════════════════════════════════"
echo ""

set C_OUT1 /tmp/c_unprocessed_nocrop.ppm
set GO_OUT1 /tmp/go_unprocessed_nocrop.ppm

echo "→ C 版本: -ppm-ascii -unprocessed -no-crop"
env DEBUG=1 $C_EXTRACT -ppm-ascii -unprocessed -no-crop -o /tmp $X3F_FILE 2>&1 | grep -E "(columns=|rows=|first pixel)" || true
mv /tmp/(basename $X3F_FILE).ppm $C_OUT1

echo ""
echo "→ Go 版本: -unprocessed -no-crop -o c-osx-universal.ppm"
env DEBUG=1 /tmp/x3f-go -unprocessed -no-crop -o $GO_OUT1 $X3F_FILE 2>&1 | tail -5

echo ""
echo "→ 对比结果:"
if python3 tests/compare_ppm.py $C_OUT1 $GO_OUT1 | grep -q "完全一致"
    echo "   ✅ 测试1 通过"
else
    echo "   ❌ 测试1 失败"
    set all_passed 0
end
echo ""

# ============================================================================
echo "═══════════════════════════════════════════════════════════════════════"
echo "  测试2: -unprocessed（默认裁剪到 ActiveImageArea）"
echo "═══════════════════════════════════════════════════════════════════════"
echo ""

set C_OUT2 /tmp/c_unprocessed_cropped.ppm
set GO_OUT2 /tmp/go_unprocessed_cropped.ppm

echo "→ C 版本: -ppm-ascii -unprocessed"
env DEBUG=1 $C_EXTRACT -ppm-ascii -unprocessed -o /tmp $X3F_FILE 2>&1 | grep -E "(columns=|rows=|first pixel)" || true
mv /tmp/(basename $X3F_FILE).ppm $C_OUT2

echo ""
echo "→ Go 版本: -unprocessed -o c-osx-universal.ppm"
env DEBUG=1 /tmp/x3f-go -unprocessed -o $GO_OUT2 $X3F_FILE 2>&1 | tail -5

echo ""
echo "→ 对比结果:"
if python3 tests/compare_ppm.py $C_OUT2 $GO_OUT2 | grep -q "完全一致"
    echo "   ✅ 测试2 通过"
else
    echo "   ❌ 测试2 失败"
    set all_passed 0
end
echo ""

# ============================================================================
echo "═══════════════════════════════════════════════════════════════════════"
echo "  测试3: -qtop（Quattro top 层数据）"
echo "═══════════════════════════════════════════════════════════════════════"
echo ""

set C_TIFF3 /tmp/c_qtop.tiff
set C_OUT3 /tmp/c_qtop.ppm
set GO_OUT3 /tmp/go_qtop.ppm

echo "→ C 版本: -tiff -qtop -no-crop"
$C_EXTRACT -tiff -qtop -no-crop -o /tmp $X3F_FILE 2>&1 | grep -v "^   :" || true
mv /tmp/(basename $X3F_FILE).tif $C_TIFF3

echo "   转换 TIFF 到 PPM..."
python3 tests/tiff_to_ppm.py $C_TIFF3 $C_OUT3

echo ""
echo "→ Go 版本: -qtop -no-crop -o c-osx-universal.ppm"
env DEBUG=1 /tmp/x3f-go -qtop -no-crop -o $GO_OUT3 $X3F_FILE 2>&1 | tail -5

echo ""
echo "→ 对比结果:"
if python3 tests/compare_ppm.py $C_OUT3 $GO_OUT3 | grep -q "完全一致"
    echo "   ✅ 测试3 通过"
else
    echo "   ❌ 测试3 失败"
    set all_passed 0
end
echo ""

# ============================================================================
echo "═══════════════════════════════════════════════════════════════════════"
echo "  最终结果"
echo "═══════════════════════════════════════════════════════════════════════"
if test $all_passed -eq 1
    echo "🎉 所有测试通过！"
    exit 0
else
    echo "❌ 有测试失败"
    exit 1
end
