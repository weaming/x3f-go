#!/usr/bin/env fish
# OpenCV 静态编译脚本 for x3f-go
# 只编译降噪需要的模块：core, imgproc, photo

set ROOT (cd (dirname (status -f)) && pwd)
set OPENCV_SRC "$ROOT/opencv"
set BUILD_DIR "$ROOT/build/opencv-static"
set INSTALL_DIR "$ROOT/build/opencv-install"

# 检测 CPU 核心数
if test (uname -s) = Darwin
    set CORES (sysctl -n hw.ncpu)
else
    set CORES (nproc 2>/dev/null; or echo 4)
end

echo "==> 使用 $CORES 核心编译"

# 检查 OpenCV 源码
if not test -d "$OPENCV_SRC/modules"
    echo "错误: OpenCV 源码目录不存在: $OPENCV_SRC"
    echo "请先运行: git submodule update --init --recursive"
    exit 1
end

# 创建构建目录
mkdir -p "$BUILD_DIR"
mkdir -p "$INSTALL_DIR"

# CMake 配置选项
set CMAKE_FLAGS \
    -D CMAKE_BUILD_TYPE=Release \
    -D CMAKE_INSTALL_PREFIX="$INSTALL_DIR" \
    \
    -D BUILD_SHARED_LIBS=OFF \
    \
    -D BUILD_LIST=core,imgproc,photo \
    \
    -D WITH_JPEG=OFF \
    -D WITH_PNG=OFF \
    -D WITH_TIFF=OFF \
    -D WITH_WEBP=OFF \
    -D WITH_OPENEXR=OFF \
    -D WITH_JASPER=OFF \
    \
    -D WITH_FFMPEG=OFF \
    -D WITH_GSTREAMER=OFF \
    -D BUILD_opencv_highgui=OFF \
    -D BUILD_opencv_videoio=OFF \
    \
    -D WITH_IPP=OFF \
    -D BUILD_opencv_python2=OFF \
    -D BUILD_opencv_python3=OFF \
    -D BUILD_opencv_java=OFF \
    -D BUILD_opencv_apps=OFF \
    \
    -D BUILD_TESTS=OFF \
    -D BUILD_PERF_TESTS=OFF \
    -D BUILD_EXAMPLES=OFF \
    -D BUILD_DOCS=OFF \
    \
    -D WITH_TBB=OFF \
    -D BUILD_TBB=OFF \
    -D WITH_PTHREADS_PF=ON \
    -D WITH_ITT=OFF \
    -D BUILD_ZLIB=ON

# macOS 特定配置
if test (uname -s) = Darwin
    set ARCH (uname -m)
    echo "==> 检测到 macOS 架构: $ARCH"

    set CMAKE_FLAGS $CMAKE_FLAGS \
        -D CMAKE_OSX_ARCHITECTURES="$ARCH" \
        -D WITH_OPENCL=OFF

    # 如果需要 Universal Binary，取消下面的注释
    set CMAKE_FLAGS $CMAKE_FLAGS -D CMAKE_OSX_ARCHITECTURES="arm64;x86_64"
end

# 配置
echo "==> 配置 OpenCV..."
cd "$BUILD_DIR"
cmake $CMAKE_FLAGS "$OPENCV_SRC"

# 编译（使用 cmake --build 跨平台兼容）
echo "==> 编译 OpenCV (这可能需要 15-30 分钟)..."
cmake --build . --config Release --parallel "$CORES"

# 安装到本地目录
echo "==> 安装静态库..."
cmake --build . --config Release --target install

echo ""
echo "==> ✅ OpenCV 静态库编译完成！"
echo ""
echo "安装位置:"
echo "  头文件: $INSTALL_DIR/include"
echo "  静态库: $INSTALL_DIR/lib"
echo ""
echo "编译的库:"
ls -lh "$INSTALL_DIR/lib/"libopencv_*.a 2>/dev/null; or echo "未找到静态库"
echo ""
echo "总大小:"
du -sh "$INSTALL_DIR/lib" 2>/dev/null

echo ""
echo "下一步："
echo "1. 在 Go 代码中使用 cgo 链接这些静态库"
echo "2. 运行 build-with-opencv.fish 编译带降噪支持的 x3f-go"
