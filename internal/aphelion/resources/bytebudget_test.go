package resources

import (
	"errors"
	"strings"
	"testing"
)

func TestBudgetReservesReleasesAndReportsAdmission(t *testing.T) {
	budget := NewFixedBudget(1 << 20)
	first, err := budget.Reserve(768 << 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budget.Reserve(300 << 10); err == nil {
		t.Fatal("over-budget edit was admitted")
	} else {
		var admission *AdmissionError
		if !errors.As(err, &admission) || admission.Needed != 300<<10 || admission.Available != 256<<10 || !strings.Contains(err.Error(), "needs about") {
			t.Fatalf("admission error lacks actionable quantities: %#v", err)
		}
	}
	first.Release()
	first.Release()
	second, err := budget.Reserve(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	second.Release()
	if got := budget.Used(); got != 0 {
		t.Fatalf("released reservations retained %d bytes", got)
	}
}

func TestAdmissionReportsIncrementalRequestAndLiveOwners(t *testing.T) {
	b := NewFixedBudget(100)
	r, err := b.Reserve(60)
	if err != nil {
		t.Fatal(err)
	}
	r.Label("document/request", "preview")
	err = r.Resize(110)
	var denial *AdmissionError
	if !errors.As(err, &denial) || !denial.Incremental || denial.Needed != 50 || denial.Reservations["document/request: preview"] != 60 {
		t.Fatal("denial lost incremental or owner accounting", err)
	}
	r.Release()
	_, err = b.Reserve(110)
	if !errors.As(err, &denial) || denial.Incremental || len(denial.Reservations) != 0 {
		t.Fatal("released owner survived", err)
	}
}
