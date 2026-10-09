package mapsave

import "sdmm/internal/dmapi/dmmsave/keygen"

// KeyLengthPlan predicts whether a save must re-key the whole file because the
// number of unique tile contents no longer fits the file's current key length.
type KeyLengthPlan struct {
	Current         int // Key length the file uses today.
	Required        int // Smallest sufficient length; 0 when even the longest is too small.
	Unique          int // Unique tile contents to be written.
	CurrentCapacity int // Distinct keys available at Current.
}

// PlanKeyLength is pure and O(1). It mirrors the save key allocator, which
// enlarges the key length once the unused keys of the current tier run out.
func PlanKeyLength(unique, current int) KeyLengthPlan {
	plan := KeyLengthPlan{Current: current, Unique: unique, CurrentCapacity: keygen.Capacity(current)}
	if plan.CurrentCapacity == 0 {
		plan.Required = current // Unknown length: the allocator makes no decision to warn about.
		return plan
	}
	for length := current; length <= keygen.MaxKeyLength; length++ {
		if unique <= keygen.Capacity(length) {
			plan.Required = length
			return plan
		}
	}
	return plan
}

// Grows reports a key length increase that rewrites every tile key.
func (p KeyLengthPlan) Grows() bool { return p.Required > p.Current && p.CurrentCapacity != 0 }

// Exceeds reports contents that no BYOND key length can hold.
func (p KeyLengthPlan) Exceeds() bool { return p.CurrentCapacity != 0 && p.Required == 0 }
