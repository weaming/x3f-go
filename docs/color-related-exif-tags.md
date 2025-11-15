DNG 文件中影响色彩表现的主要 EXIF/标签有:

**色彩矩阵类**
- **ColorMatrix1/ColorMatrix2** - 核心!把相机原生色彩空间转到 XYZ 色彩空间的矩阵,不同光源(日光/钨丝灯)各一个
- **CalibrationIlluminant1/2** - 说明上面矩阵对应什么光源

**白平衡相关**
- **AsShotNeutral** - 拍摄时的白平衡参数
- **AsShotWhiteXY** - 白平衡的 xy 色度坐标

**相机特性**
- **CameraCalibration1/2** - 微调色彩矩阵的修正矩阵
- **ForwardMatrix1/2** - ColorMatrix 的逆矩阵,从 XYZ 转回相机空间

**色调映射**
- **ProfileHueSatMapDims/Data** - HSL 色调映射表,调整特定颜色的色相/饱和度
- **ProfileToneCurve** - 基础色调曲线
- **ProfileLookTable** - "风格"预设的查找表

**渲染意图**
- **BaselineExposure** - 曝光补偿基准值
- **LinearResponseLimit** - 线性响应的上限

简单说:**ColorMatrix** 是基础,决定了色彩的"底子";**白平衡标签**决定色温;**Profile 系列标签**是后期风格化调整。

你在处理 X3F 时最需要关注的是 ColorMatrix,这个搞不准颜色就偏了。