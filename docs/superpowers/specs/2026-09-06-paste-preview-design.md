# Cancellable paste placement

Current controls (2026-10-04): Q/E rotate left/right for selections, active Grab moves, clipboard/stamp previews, and held prefabs in Add, Fill, Replace, and Move. Declared directional object variants follow the turn. The original bracket bindings below describe the earlier implementation.

This extends the existing clipboard, Grab and authoritative edit flow under the
active editor QoL goal. Default interaction: Ctrl/Cmd+V starts placement, click
or Enter confirms, Escape cancels. A shortcut preference was requested while
source investigation continued; no response was received before choosing this
default. No Git integration or protected infrastructure change is authorized.

## Behavior

Copy retains the existing copy-time visibility filter. Paste takes an independent
template from the clipboard and assigns new stable IDs to copied instances once
per placement. Moving the preview keeps those IDs; confirming commits one explicit
operation. Undo/redo preserves the resulting IDs. Hidden destination instances
retain their identities and values. Sparse clipboard holes remain untouched.

The clipboard's independent minimum X/Y is its bottom-left anchor; source Z must
be a single level and placement uses the current visible level. A placement must
fit in full, with no clipping. Invalid target positions cannot be confirmed. The
last valid preview may remain visible while an invalid target is indicated; the
toolbar explains why placement is unavailable. A template larger than the map or
operation limit is refused before changing the display.

Starting placement clears the previous selection. Confirmation selects the
destination region for existing Grab tools; cancellation clears the floating
selection and leaves the clipboard available. The floating template supports
`[` / `]` rotation and `H` / `V` mirrors around its bottom-left anchor, through
the same orientation rules as completed selections. Toolbar buttons expose the
same actions. Sparse holes remain holes; transforms retain the copied IDs and
never mutate clipboard contents. Repeated Paste keeps the current preview.

A transform validates geometry and orientation before acquiring new destination
captures. A failed capture releases only that attempt's new acquisitions. Failed
transforms retain the old template/display and block confirmation until a move or
transform succeeds; errors are shown in placement controls. A transform can make
an initially out-of-bounds target fit. Level/attachment/fault guards remain in
force, and no transform submits an operation before placement confirmation.

Escape, tool switch, map deactivation, level switch and attachment replacement
must end preview ownership. Cancellation restores only owned backgrounds and
never submits an operation. Invalid capture faults retain their existing Save
guard until validated replacement. A new destination owned by an unrelated
pending edit is refused. The preview never takes over another edit's journal.

Save and replacement/resize guards include an open placement even before it has
a valid destination. Other durable commands cannot commit an unfinished preview.
Accepted/rejected earlier network operations retain their callback/ownership
rules. Submission/rejection follows the same engine and reconciliation path as
other edits; there is no second paste authority or implicit commit on Ctrl+V.

## Implementation boundaries

Use the existing mutable display preview and renderer, with scoped background
restoration. A separate ghost renderer would add a second sprite/resource path;
immediate paste followed by a normal move would create an operation before the
user confirmed. Neither is required for this interaction.

`internal/aphelion/editing` provides a placement constructor for the existing
preview lifecycle. It owns copied source instances, exact destination membership
for sparse templates, and current-destination backgrounds. Ordinary move keeps
its source-restoration semantics. Clipboard payloads are never mutated.

Owned editor adapters bind the preview to one attachment, capture before-states,
update existing render buckets and commit only on confirmation. Narrow inherited
spans route the shipped Paste action to that adapter. The old implementation is
retained only as provenance, including its unused paste background bookkeeping.

Grab's placement state owns its editor callback target instead of resolving a
later global active editor. Frame updates occur only when the hovered tile
changes. Click/Enter confirmation and Escape respect canvas focus, active text
input and exact modifiers. Toolbar text/buttons and the F1 reference explain
placement. No new art, assets, dependencies or protocol/schema changes.

## Required evidence

Red tests must expose current hidden-ID replacement, edge clipping and absence
of cancellation. Model tests cover sparse geometry, distinct/reused IDs,
clipboard isolation, passed-over restoration, failed captures, bounds and levels.
Real hidden workspace tests cover the shipped paste/confirm/cancel seams, Save
guards, keyboard/text/focus behavior, tab/attachment disposal, network
acceptance/rejection and exact undo/redo hashes. Full repository, affected race,
desktop build and parser/loopback gates remain separate from human interaction.

Measure repeated unchanged-position handling and selected-area/background growth
without calling component allocation reduction a desktop speedup. Representative
clipboard/render/input-to-visible workloads remain in the repo-wide workplan.
