package x3f

import "math"

// ShouldApplySpatialGain 返回 C 版默认的 spatial gain 启用策略。
func (f *File) ShouldApplySpatialGain() bool {
	if f == nil || f.Header == nil {
		return false
	}

	return f.Header.Version < Version40
}

// CalcSpatialGain 按 C 版 x3f_calc_spatial_gain 计算指定像素和通道的空间增益。
func CalcSpatialGain(corrections []SpatialGainCorr, row, col, channel, rows, cols int) float64 {
	gain := 1.0
	if rows <= 0 || cols <= 0 {
		return gain
	}

	rowRelative := float64(row) / float64(rows)
	colRelative := float64(col) / float64(cols)

	for _, correction := range corrections {
		if correction.Rows <= 0 || correction.Cols <= 0 || correction.Channels <= 0 || len(correction.Gain) == 0 {
			continue
		}

		rowPitch := correction.RowPitch
		if rowPitch == 0 {
			rowPitch = 1
		}
		colPitch := correction.ColPitch
		if colPitch == 0 {
			colPitch = 1
		}

		correctionChannel := channel - correction.Chan
		if correctionChannel < 0 || correctionChannel >= correction.Channels {
			continue
		}
		if row%rowPitch != correction.RowOff {
			continue
		}
		if col%colPitch != correction.ColOff {
			continue
		}

		rowCoord := rowRelative * float64(correction.Rows-1)
		rowIndex := int(math.Floor(rowCoord))
		rowFraction := rowCoord - float64(rowIndex)

		colCoord := colRelative * float64(correction.Cols-1)
		colIndex := int(math.Floor(colCoord))
		colFraction := colCoord - float64(colIndex)

		row1Offset, row2Offset := spatialGainRowOffsets(correction, rowIndex)
		col1Offset, col2Offset := spatialGainColOffsets(correction, colIndex, correctionChannel)
		if row2Offset+col2Offset >= len(correction.Gain) {
			continue
		}

		row1Gain := float64(correction.Gain[row1Offset+col1Offset]) +
			colFraction*float64(correction.Gain[row1Offset+col2Offset]-correction.Gain[row1Offset+col1Offset])
		row2Gain := float64(correction.Gain[row2Offset+col1Offset]) +
			colFraction*float64(correction.Gain[row2Offset+col2Offset]-correction.Gain[row2Offset+col1Offset])

		gain *= row1Gain + rowFraction*(row2Gain-row1Gain)
	}

	return gain
}

func spatialGainRowOffsets(correction SpatialGainCorr, rowIndex int) (int, int) {
	rowStride := correction.Cols * correction.Channels
	if rowIndex < 0 {
		return 0, 0
	}
	if rowIndex >= correction.Rows-1 {
		offset := (correction.Rows - 1) * rowStride
		return offset, offset
	}

	return rowIndex * rowStride, (rowIndex + 1) * rowStride
}

func spatialGainColOffsets(correction SpatialGainCorr, colIndex, channel int) (int, int) {
	if colIndex < 0 {
		return channel, channel
	}
	if colIndex >= correction.Cols-1 {
		offset := (correction.Cols-1)*correction.Channels + channel
		return offset, offset
	}

	return colIndex*correction.Channels + channel,
		(colIndex+1)*correction.Channels + channel
}
