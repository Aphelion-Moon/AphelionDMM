package dmmdata

import (
	"fmt"
	"strings"
	"testing"

	"sdmm/internal/util"
)

func TestParseGridReversalPreservesDenseHolesAndDropsInvalidOrigins(t *testing.T) {
	for _, height := range []int{1, 2, 3, 4} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			var input strings.Builder
			input.WriteString("\"a\" = (/obj/test)\n\"b\" = (/obj/other)\n(0,0,0) = {\"\na\n\"}\n(2,1,2) = {\"\n")
			for row := 0; row < height; row++ {
				input.WriteString(string('a'+rune(row%2)) + "\n")
			}
			input.WriteString("\"}\n")
			data, err := parse(&testReader{strings.NewReader(input.String())})
			if err != nil {
				t.Fatal(err)
			}
			if data.MaxX != 2 || data.MaxY != height || data.MaxZ != 2 || len(data.Grid) != 4*height {
				t.Fatalf("unexpected grid dimensions or density: %dx%dx%d, %d keys", data.MaxX, data.MaxY, data.MaxZ, len(data.Grid))
			}
			for z := 1; z <= 2; z++ {
				for y := 1; y <= height; y++ {
					for x := 1; x <= 2; x++ {
						point := util.Point{X: x, Y: y, Z: z}
						want := Key("")
						if x == 2 && z == 2 {
							want = Key(string('a' + rune((height-y)%2)))
						}
						if got, found := data.Grid[point]; !found || got != want {
							t.Fatalf("grid %v = %q (present %v), want %q", point, got, found, want)
						}
					}
				}
			}
		})
	}
}

func BenchmarkParseDenseGrid(b *testing.B) {
	input := "\"a\" = (/obj/test)\n(1,1,1) = {\"\n" + strings.Repeat(strings.Repeat("a", 255)+"\n", 255) + "\"}\n"
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		data, err := parse(&testReader{strings.NewReader(input)})
		if err != nil || len(data.Grid) != 255*255 {
			b.Fatalf("parse: grid=%v err=%v", data, err)
		}
	}
}
