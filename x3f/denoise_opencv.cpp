#include <opencv2/photo.hpp>
#include <opencv2/core.hpp>
#include <opencv2/imgproc.hpp>
#include <vector>
#include <cstring>
#include <cstdint>

// O_UV 避免 U,V 负值被裁剪 (来自 Go 代码)
const int32_t O_UV = 32768;

// clampUint16 将 int32 值限制在 uint16 范围内
static inline uint16_t clampUint16(int32_t val) {
    if (val < 0) return 0;
    if (val > 65535) return 65535;
    return (uint16_t)val;
}

extern "C" {

// 定义色彩转换类型
enum ColorTransformType {
    BMT_to_YUV_STD = 0,
    YUV_to_BMT_STD,
    BMT_to_YUV_YisT,
    YUV_to_BMT_YisT,
    BMT_to_YUV_Yis4T,
    YUV_to_BMT_Yis4T,
};

// ColorTransform 在 C++ 中执行色彩空间转换
void ColorTransform(uint16_t* data, int rows, int cols, int channels, int rowStride, int transformType) {
    if (channels != 3) return;

    cv::Mat img(rows, cols, CV_16UC3, data, rowStride * sizeof(uint16_t));

    cv::parallel_for_(cv::Range(0, rows), [&](const cv::Range& range) {
        for (int r = range.start; r < range.end; ++r) {
            uint16_t* p = img.ptr<uint16_t>(r);
            for (int c = 0; c < cols; ++c) {
                int32_t B = p[0];
                int32_t M = p[1];
                int32_t T = p[2];
                int32_t Y, U, V;

                switch (transformType) {
                    case BMT_to_YUV_STD:
                        Y = (B + M + T + 1) / 3;
                        U = 2 * B - 2 * T;
                        V = B - 2 * M + T;
                        p[0] = clampUint16(Y);
                        p[1] = clampUint16(U + O_UV);
                        p[2] = clampUint16(V + O_UV);
                        break;
                    case YUV_to_BMT_STD:
                        Y = B; U = M - O_UV; V = T - O_UV;
                        p[0] = clampUint16((12 * Y + 3 * U + 2 * V + 6) / 12);
                        p[1] = clampUint16((3 * Y - V + 1) / 3);
                        p[2] = clampUint16((12 * Y - 3 * U + 2 * V + 6) / 12);
                        break;
                    case BMT_to_YUV_YisT:
                        Y = T;
                        U = 2 * B - 2 * T;
                        V = B - 2 * M + T;
                        p[0] = clampUint16(Y);
                        p[1] = clampUint16(U + O_UV);
                        p[2] = clampUint16(V + O_UV);
                        break;
                    case YUV_to_BMT_YisT:
                        Y = B; U = M - O_UV; V = T - O_UV;
                        p[0] = clampUint16((2 * Y + U + 1) / 2);
                        p[1] = clampUint16((4 * Y + U - 2 * V + 2) / 4);
                        p[2] = clampUint16(Y);
                        break;
                    case BMT_to_YUV_Yis4T:
                        Y = 4 * T;
                        U = 2 * B - 2 * T;
                        V = B - 2 * M + T;
                        p[0] = clampUint16(Y);
                        p[1] = clampUint16(U + O_UV);
                        p[2] = clampUint16(V + O_UV);
                        break;
                    case YUV_to_BMT_Yis4T:
                        Y = B; U = M - O_UV; V = T - O_UV;
                        p[0] = clampUint16((Y + 2 * U + 2) / 4);
                        p[1] = clampUint16((Y + U - 2 * V + 2) / 4);
                        p[2] = clampUint16((Y + 2) / 4);
                        break;
                }
                p += channels;
            }
        }
    });
}

void denoise_nlm_opencv(uint16_t* data, int rows, int cols, int channels, int rowStride, float h) {
    // 创建 cv::Mat，支持 stride（与 C 版本一致）
    size_t step = rowStride * sizeof(uint16_t);
    cv::Mat img(rows, cols, CV_16UC(channels), data, step);

    if (channels == 3) {
        // 完整的三步降噪流程（与 C 版本完全一致）

        // 步骤 1: 主要降噪
        cv::Mat out;
        float h1[3] = {0.0f, h, h};
        std::vector<float> h1_vec(h1, h1 + 3);
        cv::fastNlMeansDenoising(img, out, h1_vec, 3, 11, cv::NORM_L1);

        // 步骤 2: V 通道中值滤波（去除色彩噪点，如绿边）
        cv::Mat V(out.size(), CV_16U);
        int get_V[2] = {2, 0};  // 提取第 2 个通道（V）
        int set_V[2] = {0, 2};  // 放回第 2 个通道
        cv::mixChannels(&out, 1, &V, 1, get_V, 1);
        cv::medianBlur(V, V, 3);
        cv::mixChannels(&V, 1, &out, 1, set_V, 1);

        // 步骤 3: 低频降噪
        cv::Mat sub, sub_dn, sub_res, res;
        float h2[3] = {0.0f, h/8, h/4};
        std::vector<float> h2_vec(h2, h2 + 3);

        cv::resize(out, sub, cv::Size(), 1.0/4, 1.0/4, cv::INTER_AREA);
        cv::fastNlMeansDenoising(sub, sub_dn, h2_vec, 3, 21, cv::NORM_L1);
        cv::subtract(sub, sub_dn, sub_res, cv::noArray(), CV_16S);
        cv::resize(sub_res, res, out.size(), 0.0, 0.0, cv::INTER_CUBIC);
        cv::subtract(out, res, out, cv::noArray(), CV_16U);

        // 复制回原始数据（支持 stride）
        for (int y = 0; y < rows; y++) {
            memcpy(data + y * rowStride,
                   out.ptr<uint16_t>(y),
                   cols * channels * sizeof(uint16_t));
        }
    } else {
        // 单通道降噪（简化版）
        cv::Mat dst;
        std::vector<float> h_vector(1, h);
        cv::fastNlMeansDenoising(img, dst, h_vector, 3, 11, cv::NORM_L1);

        // 复制回原始数据（支持 stride）
        for (int y = 0; y < rows; y++) {
            memcpy(data + y * rowStride,
                   dst.ptr<uint16_t>(y),
                   cols * channels * sizeof(uint16_t));
        }
    }
}

// Quattro 高分辨率降噪（简化版，只执行一次 fastNlMeansDenoising）
// 对应 C 版本 x3f_expand_quattro 中对 active_exp 的处理
void denoise_quattro_highres_opencv(uint16_t* data, int rows, int cols, int channels, int rowStride, float h) {
    // 创建 cv::Mat，支持 stride
    size_t step = rowStride * sizeof(uint16_t);
    cv::Mat img(rows, cols, CV_16UC(channels), data, step);

    if (channels == 3) {
        // 高分辨率 Quattro 降噪：只执行一次 fastNlMeansDenoising
        // 注意：V 通道使用 h*2 的强度（与 C 版本一致）
        cv::Mat out;
        float h_values[3] = {0.0f, h, h*2};  // Y=0, U=h, V=h*2
        std::vector<float> h_vec(h_values, h_values + 3);
        cv::fastNlMeansDenoising(img, out, h_vec, 3, 11, cv::NORM_L1);

        // 复制回原始数据（支持 stride）
        for (int y = 0; y < rows; y++) {
            memcpy(data + y * rowStride,
                   out.ptr<uint16_t>(y),
                   cols * channels * sizeof(uint16_t));
        }
    } else {
        // 单通道降噪
        cv::Mat dst;
        std::vector<float> h_vector(1, h);
        cv::fastNlMeansDenoising(img, dst, h_vector, 3, 11, cv::NORM_L1);

        // 复制回原始数据（支持 stride）
        for (int y = 0; y < rows; y++) {
            memcpy(data + y * rowStride,
                   dst.ptr<uint16_t>(y),
                   cols * channels * sizeof(uint16_t));
        }
    }
}

// Bicubic 上采样（使用 OpenCV，与 C 版本完全一致）
void bicubic_upscale_opencv(uint16_t* src, int srcRows, int srcCols, int channels, int srcStride,
                             uint16_t* dst, int dstRows, int dstCols, int dstStride) {
    // 创建源图像 Mat
    cv::Mat srcMat(srcRows, srcCols, CV_16UC(channels), src, srcStride * sizeof(uint16_t));

    // 创建目标图像 Mat
    cv::Mat dstMat(dstRows, dstCols, CV_16UC(channels), dst, dstStride * sizeof(uint16_t));

    // 使用 OpenCV resize，与 C 版本完全一致
    cv::resize(srcMat, dstMat, dstMat.size(), 0.0, 0.0, cv::INTER_CUBIC);
}


} // extern "C"
