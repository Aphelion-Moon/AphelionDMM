package mapsave

import "testing"

func TestPlanKeyLengthBoundaries(t *testing.T) {
	cases := []struct {
		name            string
		unique, current int
		required        int
		capacity        int
		grows, exceeds  bool
	}{
		{"len1 capacity-1", 51, 1, 1, 52, false, false},
		{"len1 capacity", 52, 1, 1, 52, false, false},
		{"len1 capacity+1", 53, 1, 2, 52, true, false},
		{"len2 capacity-1", 2703, 2, 2, 2704, false, false},
		{"len2 capacity", 2704, 2, 2, 2704, false, false},
		{"len2 capacity+1", 2705, 2, 3, 2704, true, false},
		{"len3 capacity", 65529, 3, 3, 65529, false, false},
		{"len3 capacity+1", 65530, 3, 0, 65529, false, true},
		{"skips a length", 2705, 1, 3, 52, true, false},
		{"empty map", 0, 2, 2, 2704, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := PlanKeyLength(tc.unique, tc.current)
			if plan.Current != tc.current || plan.Unique != tc.unique || plan.Required != tc.required ||
				plan.CurrentCapacity != tc.capacity || plan.Grows() != tc.grows || plan.Exceeds() != tc.exceeds {
				t.Fatalf("PlanKeyLength(%d,%d) = %+v", tc.unique, tc.current, plan)
			}
		})
	}
}

func TestPlanKeyLengthUnknownCurrentLength(t *testing.T) {
	if plan := PlanKeyLength(10, 0); plan.Grows() || plan.Exceeds() {
		t.Fatalf("unknown current length must not warn: %+v", plan)
	}
}
