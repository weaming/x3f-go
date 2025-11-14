#!/usr/bin/env fish

# 跨平台静态编译 x3f-go（带 OpenCV 支持）
#
# 注意：CGO 跨平台交叉编译非常复杂，需要目标平台的：
# 1. 交叉编译工具链
# 2. OpenCV 静态库
# 3. 系统库
#
# 此脚本只编译当前平台的多架构版本：
# - macOS: Universal Binary (arm64 + amd64)
# - Linux: amd64 和 arm64（如果支持）

set ROOT (cd (dirname (status -f)) && pwd)
set BUILD_DIR "$ROOT/build/release"
set INSTALL_DIR "$ROOT/build/opencv-install"
set CMD_PATH ./cmd/x3f-go
set LDFLAGS "-s -w"

# 检测操作系统
set OS (uname -s)
set ARCH (uname -m)

echo "=== x3f-go 静态编译工具 ==="
echo "当前平台: $OS / $ARCH"
echo ""

# 检查 OpenCV 静态库
if not test -f "$INSTALL_DIR/lib/libopencv_core.a"
    fish build-opencv-static.fish
end

echo "✅ OpenCV 静态库已找到"
echo ""

# 清理并创建 build 目录
echo "清理构建目录..."
rm -rf $BUILD_DIR
mkdir -p $BUILD_DIR

# 设置 CGO 环境变量
set -x CGO_ENABLED 1

echo ""
echo "=== 开始编译 ==="
echo ""

switch $OS
    case Darwin
        # ===== macOS =====
        echo "平台: macOS"
        echo ""

        # 检查是否在 CI 环境中（GitHub Actions 设置 CI=true）
        if set -q CI
            echo "检测到 CI 环境，构建 Universal Binary"

            # macOS arm64
            echo "[1/3] 编译 darwin/arm64..."
            env GOOS=darwin GOARCH=arm64 \
                go build -ldflags="$LDFLAGS" \
                -o $BUILD_DIR/x3f-go-darwin-arm64 $CMD_PATH

            if test $status -eq 0
                echo "  ✅ darwin/arm64 编译成功"
            else
                echo "  ❌ darwin/arm64 编译失败"
                exit 1
            end

            # macOS amd64
            echo "[2/3] 编译 darwin/amd64..."
            env GOOS=darwin GOARCH=amd64 \
                go build -ldflags="$LDFLAGS" \
                -o $BUILD_DIR/x3f-go-darwin-amd64 $CMD_PATH

            if test $status -eq 0
                echo "  ✅ darwin/amd64 编译成功"
            else
                echo "  ❌ darwin/amd64 编译失败"
                exit 1
            end

            # 合并为 Universal Binary
            echo "[3/3] 合并为 Universal Binary..."
            lipo -create -output $BUILD_DIR/x3f-go-darwin-universal \
                $BUILD_DIR/x3f-go-darwin-arm64 \
                $BUILD_DIR/x3f-go-darwin-amd64

            if test $status -eq 0
                echo "  ✅ Universal Binary 创建成功"
                rm $BUILD_DIR/x3f-go-darwin-arm64 $BUILD_DIR/x3f-go-darwin-amd64
            else
                echo "  ❌ lipo 合并失败"
                exit 1
            end
        else
            echo "本地开发环境，只编译当前架构: $ARCH"

            # 只编译当前架构
            echo "[1/1] 编译 darwin/$ARCH..."
            env GOOS=darwin GOARCH=$ARCH \
                go build -ldflags="$LDFLAGS" \
                -o $BUILD_DIR/x3f-go-darwin-$ARCH $CMD_PATH

            if test $status -eq 0
                echo "  ✅ darwin/$ARCH 编译成功"
            else
                echo "  ❌ darwin/$ARCH 编译失败"
                exit 1
            end
        end

    case Linux
        # ===== Linux =====
        echo "平台: Linux"
        echo ""

        # Linux amd64 (完全静态链接)
        echo "[1/2] 编译 linux/amd64..."
        env GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
            go build -ldflags="$LDFLAGS -linkmode external -extldflags '-static -pthread'" \
            -tags 'osusergo netgo static_build' \
            -o $BUILD_DIR/x3f-go-linux-amd64 $CMD_PATH

        if test $status -eq 0
            echo "  ✅ linux/amd64 编译成功"
        else
            echo "  ❌ linux/amd64 编译失败"
        end

        # Linux arm64 (需要交叉编译工具链)
        if command -v aarch64-linux-gnu-gcc > /dev/null
            echo "[2/2] 编译 linux/arm64..."
            env GOOS=linux GOARCH=arm64 CGO_ENABLED=1 \
                CC=aarch64-linux-gnu-gcc \
                go build -ldflags="$LDFLAGS -linkmode external -extldflags '-static -pthread'" \
                -tags 'osusergo netgo static_build' \
                -o $BUILD_DIR/x3f-go-linux-arm64 $CMD_PATH

            if test $status -eq 0
                echo "  ✅ linux/arm64 编译成功"
            else
                echo "  ❌ linux/arm64 编译失败"
            end
        else
            echo "[2/2] 跳过 linux/arm64 (需要 aarch64-linux-gnu-gcc)"
        end

    case '*'
        echo "❌ 不支持的操作系统: $OS"
        echo "仅支持 macOS 和 Linux"
        exit 1
end

# 显示构建结果
echo ""
echo "=== 构建完成 ==="
echo ""
ls -lh $BUILD_DIR/
echo ""

# 显示文件信息（如果 file 命令存在）
if command -v file > /dev/null
    echo "文件信息:"
    file $BUILD_DIR/*
    echo ""
end

echo "总计:"
du -sh $BUILD_DIR
echo ""

# 显示跨平台编译说明
echo "💡 跨平台编译说明："
echo ""
switch $OS
    case Darwin
        if set -q CI
            echo "已编译: macOS Universal Binary (arm64 + amd64)"
        else
            echo "已编译: macOS $ARCH"
            echo ""
            echo "如需 Universal Binary："
            echo "  - 推送到 GitHub 并创建 release 标签"
            echo "  - 或设置 CI 环境变量: set -x CI true"
        end
        echo ""
        echo "如需其他平台："
        echo "  - Linux: 在 Linux 机器上运行此脚本"
        echo "  - 或使用 GitHub Actions 进行跨平台构建"
    case Linux
        echo "已编译: Linux 版本"
        echo ""
        echo "如需其他平台："
        echo "  - macOS: 在 macOS 机器上运行此脚本"
end

echo ""
echo "✅ 全部完成！"
