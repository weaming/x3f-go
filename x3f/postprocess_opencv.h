#ifndef POSTPROCESS_OPENCV_H
#define POSTPROCESS_OPENCV_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

void applyPostProcessingOpenCV(
    uint16_t* input,
    double* output,
    int width,
    int height,
    double exposure,
    int toneMappingMethod,
    double gamma
);

#ifdef __cplusplus
}
#endif

#endif // POSTPROCESS_OPENCV_H
