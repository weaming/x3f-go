#include <opencv2/photo.hpp>
#include <opencv2/core.hpp>
#include <opencv2/imgproc.hpp>
#include <vector>
#include <cstring>

extern "C" {

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
