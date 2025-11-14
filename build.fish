#!/usr/bin/env fish
# 编译 x3f-go（带 OpenCV 支持）

set ROOT (cd (dirname (status -f)) && pwd)
set INSTALL_DIR "$ROOT/build/opencv-install"

# 检查 OpenCV 静态库是否存在
if not test -f "$INSTALL_DIR/lib/libopencv_core.a"
    echo "==> OpenCV 静态库未找到，开始编译..."
    ./build-opencv-static.fish
    or begin
        echo "错误: OpenCV 编译失败"
        exit 1
    end
end

echo "==> 编译 x3f-go..."
echo ""

# 设置 CGO 环境变量
set -x CGO_ENABLED 1

# 编译
go build -ldflags="-s -w" -o build/x3f-go ./cmd/x3f-go/

if test $status -eq 0
    echo ""
    echo "==> ✅ 编译成功！"
    echo ""
    ls -lh build/x3f-go
else
    echo ""
    echo "==> ❌ 编译失败"
    exit 1
end
