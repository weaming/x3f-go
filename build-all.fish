#!/usr/bin/env fish

# 为所有平台编译 x3f-go

set VERSION 0.1.0
set BUILD_DIR build
set CMD_PATH ./cmd/x3f-go
set LDFLAGS "-s -w  -X main.Version=$VERSION"

echo "清理并创建 build 目录..."
rm -rf $BUILD_DIR
mkdir -p $BUILD_DIR

echo ""
echo "=== macOS (Universal Binary) ==="

# macOS arm64
echo "编译 darwin/arm64..."
env GOOS=darwin GOARCH=arm64 go build -ldflags="$LDFLAGS" -o $BUILD_DIR/x3f-go-darwin-arm64 $CMD_PATH
if test $status -eq 0
    echo "  ✓ darwin/arm64"
else
    echo "  ✗ 编译失败: darwin/arm64"
    exit 1
end

# macOS amd64
echo "编译 darwin/amd64..."
env GOOS=darwin GOARCH=amd64 go build -ldflags="$LDFLAGS" -o $BUILD_DIR/x3f-go-darwin-amd64 $CMD_PATH
if test $status -eq 0
    echo "  ✓ darwin/amd64"
else
    echo "  ✗ 编译失败: darwin/amd64"
    exit 1
end

# 合并为 Universal Binary
echo "合并为 Universal Binary..."
lipo -create -output $BUILD_DIR/x3f-go-darwin-universal \
    $BUILD_DIR/x3f-go-darwin-arm64 \
    $BUILD_DIR/x3f-go-darwin-amd64

if test $status -eq 0
    echo "  ✓ darwin/universal"
    rm $BUILD_DIR/x3f-go-darwin-arm64 $BUILD_DIR/x3f-go-darwin-amd64
else
    echo "  ✗ lipo 失败"
    exit 1
end

echo ""
echo "=== Linux ==="

# Linux amd64
echo "编译 linux/amd64..."
env GOOS=linux GOARCH=amd64 go build -ldflags="$LDFLAGS" -o $BUILD_DIR/x3f-go-linux-amd64 $CMD_PATH
if test $status -eq 0
    echo "  ✓ linux/amd64"
else
    echo "  ✗ 编译失败: linux/amd64"
end

# Linux arm64
echo "编译 linux/arm64..."
env GOOS=linux GOARCH=arm64 go build -ldflags="$LDFLAGS" -o $BUILD_DIR/x3f-go-linux-arm64 $CMD_PATH
if test $status -eq 0
    echo "  ✓ linux/arm64"
else
    echo "  ✗ 编译失败: linux/arm64"
end

echo ""
echo "=== Windows ==="

# Windows amd64
echo "编译 windows/amd64..."
env GOOS=windows GOARCH=amd64 go build -ldflags="$LDFLAGS" -o $BUILD_DIR/x3f-go-windows-amd64.exe $CMD_PATH
if test $status -eq 0
    echo "  ✓ windows/amd64"
else
    echo "  ✗ 编译失败: windows/amd64"
end

# Windows arm64
echo "编译 windows/arm64..."
env GOOS=windows GOARCH=arm64 go build -ldflags="$LDFLAGS" -o $BUILD_DIR/x3f-go-windows-arm64.exe $CMD_PATH
if test $status -eq 0
    echo "  ✓ windows/arm64"
else
    echo "  ✗ 编译失败: windows/arm64"
end

# 显示构建结果
echo ""
echo "=== 构建完成 ==="
echo ""
ls -lh $BUILD_DIR/
echo ""
file $BUILD_DIR/*
echo ""
echo "总计:"
du -sh $BUILD_DIR
