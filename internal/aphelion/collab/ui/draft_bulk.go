package ui

import (
	"errors"
	"fmt"
	"strings"

	"sdmm/internal/aphelion/collab/model"
)

// BulkDraftAction is a resolution applied to every retained draft.
type BulkDraftAction string

const (
	BulkExportAll  BulkDraftAction = "export_all"
	BulkDiscardAll BulkDraftAction = "discard_all"
	BulkRefreshAll BulkDraftAction = "refresh_all"
)

// ErrBulkCancelled is returned by a resolver when the user declines an
// interactive step (for example the export destination). It stops the run
// without being reported as a failure.
var ErrBulkCancelled = errors.New("bulk draft action cancelled")

// DraftResolver applies one per-draft action. The app's existing per-draft
// resolvers are adapted to this shape so bulk actions never reimplement them.
type DraftResolver func(model.OperationID, ConflictAction) error

// BulkOutcome reports how far a bulk action got. Remaining counts drafts left
// unresolved by this run, including the one that failed.
type BulkOutcome struct {
	Action       BulkDraftAction
	Total        int
	Completed    int
	Remaining    int
	FailedID     model.OperationID
	FailedAction ConflictAction
	Err          error
	Cancelled    bool
}

// RunBulkDraftAction applies the per-draft resolver, in order, over every id
// and stops at the first failure. Discard exports every draft before
// discarding any when exportFirst is set, so a failed export never loses data.
func RunBulkDraftAction(action BulkDraftAction, exportFirst bool, ids []model.OperationID, resolve DraftResolver) BulkOutcome {
	outcome := BulkOutcome{Action: action, Total: len(ids), Remaining: len(ids)}
	if len(ids) == 0 {
		return outcome
	}
	step := func(id model.OperationID, perDraft ConflictAction) bool {
		err := resolve(id, perDraft)
		if err == nil {
			return true
		}
		outcome.FailedID, outcome.FailedAction = id, perDraft
		if errors.Is(err, ErrBulkCancelled) {
			outcome.Cancelled = true
		} else {
			outcome.Err = err
		}
		return false
	}
	switch action {
	case BulkExportAll:
		for _, id := range ids {
			if !step(id, ConflictActionExport) {
				return outcome
			}
			outcome.Completed++
			outcome.Remaining--
		}
	case BulkRefreshAll:
		for _, id := range ids {
			if !step(id, ConflictActionRefresh) {
				return outcome
			}
			outcome.Completed++
			outcome.Remaining--
		}
	case BulkDiscardAll:
		if exportFirst {
			for _, id := range ids {
				if !step(id, ConflictActionExport) {
					return outcome
				}
			}
		}
		for _, id := range ids {
			if !step(id, ConflictActionDiscard) {
				return outcome
			}
			outcome.Completed++
			outcome.Remaining--
		}
	default:
		outcome.Err = fmt.Errorf("unsupported bulk draft action %q", action)
	}
	return outcome
}

func (action BulkDraftAction) label() string {
	switch action {
	case BulkExportAll:
		return "Export all"
	case BulkDiscardAll:
		return "Discard all"
	case BulkRefreshAll:
		return "Refresh all"
	default:
		return string(action)
	}
}

// Failed reports whether the run ended before every draft was handled.
func (outcome BulkOutcome) Failed() bool { return outcome.Err != nil || outcome.Cancelled }

// Message describes a stopped run for the user. It is empty when nothing needs
// reporting.
func (outcome BulkOutcome) Message() string {
	if !outcome.Failed() {
		return ""
	}
	var message strings.Builder
	if outcome.Cancelled {
		fmt.Fprintf(&message, "%s cancelled at draft %s (%d of %d completed).", outcome.Action.label(), outcome.FailedID, outcome.Completed, outcome.Total)
	} else {
		fmt.Fprintf(&message, "%s stopped at draft %s during %s (%d of %d completed): %v.", outcome.Action.label(), outcome.FailedID, outcome.FailedAction, outcome.Completed, outcome.Total, outcome.Err)
	}
	fmt.Fprintf(&message, " %d drafts remain unresolved and were not changed.", outcome.Remaining)
	return message.String()
}

// DraftExportFileName builds a flat, filesystem-safe file name for a bulk
// export so every draft in a folder is distinct.
func DraftExportFileName(index int, id model.OperationID) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			return r
		default:
			return '_'
		}
	}, string(id))
	if len(safe) > 64 {
		safe = safe[:64]
	}
	return fmt.Sprintf("collaboration-draft-%03d-%s.json", index+1, safe)
}

// DraftPromptGate decides when the "Unsent collaboration changes" modal opens
// for an involuntary disconnect. It opens once per disconnect episode: after
// the first request it stays quiet until the interruption ends (recovery, no
// drafts left) or the session changes.
type DraftPromptGate struct {
	session string
	shown   bool
}

// Observe returns true when the prompt should be opened now.
func (gate *DraftPromptGate) Observe(view ViewModel) bool {
	if view.SessionLabel != gate.session {
		gate.session, gate.shown = view.SessionLabel, false
	}
	if !view.DraftsInterrupted {
		gate.shown = false
		return false
	}
	if gate.shown {
		return false
	}
	gate.shown = true
	return true
}

// Dismiss records that the user chose to keep the drafts for later. The
// episode stays marked as shown, so the prompt does not return until it ends.
func (gate *DraftPromptGate) Dismiss() { gate.shown = true }

// Reset forgets the episode, for example when the session is left.
func (gate *DraftPromptGate) Reset() { *gate = DraftPromptGate{} }
