package services

// day_utils_coverage_test.go — covers RemoveUint, which had no direct test
// (gremlins reported the filter comparison on line 166 as NOT COVERED).

import "testing"

func TestDayutilsCovRemoveUint(t *testing.T) {
	cases := []struct {
		name   string
		values []uint
		needle uint
		want   []uint
	}{
		{name: "removes the matching element", values: []uint{1, 2, 3}, needle: 2, want: []uint{1, 3}},
		{name: "removes every occurrence", values: []uint{4, 4, 5, 4}, needle: 4, want: []uint{5}},
		{name: "keeps all when needle absent", values: []uint{7, 8, 9}, needle: 1, want: []uint{7, 8, 9}},
		{name: "empty input yields empty", values: []uint{}, needle: 3, want: []uint{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RemoveUint(tc.values, tc.needle)
			if len(got) != len(tc.want) {
				t.Fatalf("RemoveUint(%v, %d) = %v, want %v", tc.values, tc.needle, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("RemoveUint(%v, %d) = %v, want %v", tc.values, tc.needle, got, tc.want)
				}
			}
		})
	}
}
