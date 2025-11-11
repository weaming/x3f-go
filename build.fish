#!/usr/bin/env fish

# X3F Go 版本构建脚本

set SCRIPT_DIR (dirname (status -f))
cd $SCRIPT_DIR

set OUTPUT_DIR ./build
set OUTPUT_NAME x3f-go

echo "=== 构建 X3F Go 版本 ==="
echo "输出目录: $OUTPUT_DIR"
echo "可执行文件: $OUTPUT_NAME"

mkdir -p $OUTPUT_DIR

go build -o $OUTPUT_DIR/$OUTPUT_NAME ./cmd/x3f-go

echo "✓ 构建完成: $OUTPUT_DIR/$OUTPUT_NAME"
echo ""
ls -lh $OUTPUT_DIR/$OUTPUT_NAME
