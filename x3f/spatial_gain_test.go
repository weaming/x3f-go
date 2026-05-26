package x3f

import "testing"

func TestShouldApplySpatialGainMatchesX3FToolsDefault(t *testing.T) {
	classicFile := &File{Header: &FileHeader{Version: Version30}}
	if !classicFile.ShouldApplySpatialGain() {
		t.Fatalf("classic X3F should apply spatial gain by default")
	}

	quattroFile := &File{Header: &FileHeader{Version: Version40}}
	if quattroFile.ShouldApplySpatialGain() {
		t.Fatalf("Quattro X3F should not apply spatial gain by default")
	}
}

func TestCalcSpatialGainBilinearInterpolatesGainMap(t *testing.T) {
	corrections := []SpatialGainCorr{
		{
			Gain:     []float32{1, 2, 3, 4},
			Rows:     2,
			Cols:     2,
			Channels: 1,
			RowPitch: 1,
			ColPitch: 1,
			Chan:     0,
		},
	}

	gain := CalcSpatialGain(corrections, 50, 50, 0, 100, 100)
	if gain != 2.5 {
		t.Fatalf("gain = %f, want 2.5", gain)
	}
}

func TestCalcSpatialGainHonorsChannelOffsetAndPitch(t *testing.T) {
	corrections := []SpatialGainCorr{
		{
			Gain:     []float32{2},
			Rows:     1,
			Cols:     1,
			Channels: 1,
			RowOff:   1,
			ColOff:   1,
			RowPitch: 2,
			ColPitch: 2,
			Chan:     2,
		},
	}

	matchedGain := CalcSpatialGain(corrections, 1, 1, 2, 4, 4)
	if matchedGain != 2 {
		t.Fatalf("matched gain = %f, want 2", matchedGain)
	}

	skippedGain := CalcSpatialGain(corrections, 0, 1, 2, 4, 4)
	if skippedGain != 1 {
		t.Fatalf("skipped gain = %f, want 1", skippedGain)
	}
}
