package pdfgen

import "testing"

func TestTileLayout(t *testing.T) {
	tests := []struct {
		name          string
		page, tile, n int
		wantTiles     int
		wantLast      int
		wantTruncated bool
	}{
		{"1枚に収まる", 1000, 1600, 6, 1, 1000, false},
		{"ちょうど2枚", 3200, 1600, 6, 2, 1600, false},
		{"端数あり", 4000, 1600, 6, 3, 800, false},
		{"上限で切り捨て", 20000, 1600, 6, 6, 1600, true},
		{"高さ0", 0, 1600, 6, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tiles, truncated := tileLayout(tt.page, tt.tile, tt.n)
			if len(tiles) != tt.wantTiles || truncated != tt.wantTruncated {
				t.Fatalf("tiles = %d, truncated = %v", len(tiles), truncated)
			}
			if len(tiles) > 0 && tiles[len(tiles)-1][1] != tt.wantLast {
				t.Errorf("最後の高さ = %d, want %d", tiles[len(tiles)-1][1], tt.wantLast)
			}
		})
	}
}
