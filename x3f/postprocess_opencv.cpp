#include "postprocess_opencv.h"
#include "agx_lut_data.h"
#include <opencv2/core.hpp>
#include <opencv2/imgproc.hpp>
#include <cmath>
#include <vector>

namespace {

// Clamp a value between min and max
template<typename T>
inline T clamp(T value, T min, T max) {
    if (value < min) return min;
    if (value > max) return max;
    return value;
}

// ACES tone mapping implementation for a single color
cv::Vec3d ACESToneMapping(const cv::Vec3d& color) {
    const double a = 2.51;
    const double b = 0.03;
    const double c = 2.43;
    const double d = 0.59;
    const double e = 0.14;

    cv::Vec3d result;
    for (int i = 0; i < 3; ++i) {
        double x = color[i];
        result[i] = clamp((x * (a * x + b)) / (x * (c * x + d) + e), 0.0, 1.0);
    }
    return result;
}

// Trilinear interpolation for the AgX LUT
cv::Vec3f agxLUT3DLookup(const cv::Vec3d& color) {
    const double size = static_cast<double>(AgX_LUT_SIZE);

    // Map [0,1] to LUT coordinates
    double r = color[0] * (size - 1.0);
    double g = color[1] * (size - 1.0);
    double b = color[2] * (size - 1.0);

    int r0 = static_cast<int>(floor(r));
    int g0 = static_cast<int>(floor(g));
    int b0 = static_cast<int>(floor(b));

    int r1 = std::min(r0 + 1, AgX_LUT_SIZE - 1);
    int g1 = std::min(g0 + 1, AgX_LUT_SIZE - 1);
    int b1 = std::min(b0 + 1, AgX_LUT_SIZE - 1);

    double dr = r - r0;
    double dg = g - g0;
    double db = b - b0;

    auto getValue = [&](int ir, int ig, int ib) {
        int index = ir + ig * AgX_LUT_SIZE + ib * AgX_LUT_SIZE * AgX_LUT_SIZE;
        return cv::Vec3f(AgX_LUT_Data[index][0], AgX_LUT_Data[index][1], AgX_LUT_Data[index][2]);
    };

    cv::Vec3f c000 = getValue(r0, g0, b0);
    cv::Vec3f c001 = getValue(r0, g0, b1);
    cv::Vec3f c010 = getValue(r0, g1, b0);
    cv::Vec3f c011 = getValue(r0, g1, b1);
    cv::Vec3f c100 = getValue(r1, g0, b0);
    cv::Vec3f c101 = getValue(r1, g0, b1);
    cv::Vec3f c110 = getValue(r1, g1, b0);
    cv::Vec3f c111 = getValue(r1, g1, b1);

    cv::Vec3f result;
    for (int i = 0; i < 3; ++i) {
        double c00 = c000[i] * (1.0 - dr) + c100[i] * dr;
        double c01 = c001[i] * (1.0 - dr) + c101[i] * dr;
        double c10 = c010[i] * (1.0 - dr) + c110[i] * dr;
        double c11 = c011[i] * (1.0 - dr) + c111[i] * dr;

        double c0 = c00 * (1.0 - dg) + c10 * dg;
        double c1 = c01 * (1.0 - dg) + c11 * dg;

        result[i] = static_cast<float>(c0 * (1.0 - db) + c1 * db);
    }
    return result;
}


// AgX tone mapping implementation for a single color
cv::Vec3d AgXToneMapping(const cv::Vec3d& color) {
    // sRGB to E-Gamut matrix
    cv::Matx33d egamutMatrix(
        0.856627153315983, 0.0951212405381588, 0.0482516061458583,
        0.137318972929847, 0.761241990602591, 0.101439036467562,
        0.11189821299995, 0.0767994186031903, 0.811302368396859
    );
    cv::Vec3d egamut = egamutMatrix * color;

    // Log2 encode
    const double minLog = -12.47393;
    const double maxLog = 12.5260688117;
    cv::Vec3d logEncoded;
    for (int i = 0; i < 3; ++i) {
        if (egamut[i] <= 0) {
            logEncoded[i] = 0;
        } else {
            double logVal = log2(egamut[i]);
            logEncoded[i] = clamp((logVal - minLog) / (maxLog - minLog), 0.0, 1.0);
        }
    }

    // 3D LUT lookup
    cv::Vec3f lutResult = agxLUT3DLookup(logEncoded);

    // The LUT output is already display-encoded (gamma 2.2 like).
    // To fit into the existing pipeline which applies gamma later,
    // we convert it back to linear space.
    cv::Vec3d result;
    for (int i = 0; i < 3; ++i) {
        result[i] = clamp(pow(static_cast<double>(lutResult[i]), 2.2), 0.0, 1.0);
    }
    return result;
}


} // anonymous namespace

extern "C" {

void applyPostProcessingOpenCV(
    uint16_t* input,
    double* output,
    int width,
    int height,
    double exposure,
    int toneMappingMethod, // 0=None, 1=ACES, 2=AgX
    double gamma
) {
    cv::Mat inputMat(height, width, CV_16UC3, input);
    cv::Mat outputMat(height, width, CV_64FC3, output);

    // Normalize to [0, 1]
    inputMat.convertTo(outputMat, CV_64FC3, 1.0 / 65535.0);

    // Apply exposure compensation
    if (exposure != 0.0) {
        outputMat *= pow(2.0, exposure);
    }

    // Apply tone mapping and gamma correction
    if (toneMappingMethod != 0 || gamma != 1.0) {
        for (int y = 0; y < height; ++y) {
            for (int x = 0; x < width; ++x) {
                cv::Vec3d& pixel = outputMat.at<cv::Vec3d>(y, x);

                // Tone Mapping
                if (toneMappingMethod == 1) { // ACES
                    pixel = ACESToneMapping(pixel);
                } else if (toneMappingMethod == 2) { // AgX
                    pixel = AgXToneMapping(pixel);
                }

                // Gamma Correction
                if (gamma > 0.0 && gamma != 1.0) {
                    for(int i=0; i<3; ++i) {
                        if (pixel[i] > 0) {
                            pixel[i] = pow(pixel[i], 1.0 / gamma);
                        }
                    }
                }
            }
        }
    }
}

} // extern "C"
