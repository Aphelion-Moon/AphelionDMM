package ui

import (
	"context"
	"time"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/repoinfo"
	w "sdmm/internal/imguiext/widget"
)

const localInspectionTimeout = 20 * time.Second

// browserRepository holds the joiner's local checkout inspection. It belongs to
// the UI thread; the inspection itself runs through Browser.Run and publishes
// only through Browser.Schedule.
type browserRepository struct {
	sessionID  string
	busy       bool
	done       bool
	err        string
	local      LocalEnvironment
	generation uint64
}

func (b *Browser) run(f func()) {
	if b.Run != nil {
		b.Run(f)
		return
	}
	go f()
}

// Recheck forgets the previous comparison so the next frame inspects the
// checkout again, for example after the user switched commits and reloaded.
func (b *Browser) Recheck() {
	b.repo.sessionID = ""
	b.repo.done = false
}

func (b *Browser) selectedSummary() *protocol.HostedSessionSummary {
	for i := range b.page.Sessions {
		if b.page.Sessions[i].SessionID == b.selected {
			return &b.page.Sessions[i]
		}
	}
	return nil
}

func (b *Browser) ensureInspection(sessionID string) {
	if b.LocalEnvironment == nil || b.repo.sessionID == sessionID {
		return
	}
	b.repo.generation++
	generation := b.repo.generation
	b.repo.sessionID, b.repo.busy, b.repo.done, b.repo.err = sessionID, true, false, ""
	inspect := b.LocalEnvironment() // UI thread: bind the currently loaded environment.
	b.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), localInspectionTimeout)
		defer cancel()
		local, err := inspect(ctx)
		b.Schedule(func() {
			if b.closed || b.repo.generation != generation {
				return
			}
			b.repo.busy, b.repo.done = false, true
			if err != nil {
				b.repo.err = err.Error()
				return
			}
			b.repo.local = local
		})
	})
}

// alignmentFor reports the comparison only for the session it was computed for.
func (b *Browser) alignmentFor(sessionID string) (AlignmentReport, bool) {
	if b.repo.sessionID != sessionID || !b.repo.done || b.repo.err != "" {
		return AlignmentReport{}, false
	}
	for i := range b.page.Sessions {
		if b.page.Sessions[i].SessionID == sessionID && b.page.Sessions[i].Repository != nil {
			return BuildAlignmentReport(*b.page.Sessions[i].Repository, b.repo.local), true
		}
	}
	return AlignmentReport{}, false
}

func (b *Browser) copyAlignment(sessionID string) {
	report, ok := b.alignmentFor(sessionID)
	if !ok || b.CopyText == nil {
		return
	}
	if text := report.Alignment.CopyText(); text != "" {
		b.CopyText(text)
	}
}

func (b *Browser) showRepository(selected *protocol.HostedSessionSummary) {
	imgui.Separator()
	imgui.Text("Host repository")
	if selected.Repository == nil {
		imgui.TextWrapped("Unknown: the host did not publish a repository descriptor (older editor or service, or the service restarted).")
		return
	}
	imgui.TextWrapped(DescribeRepository(selected.Repository))
	if b.LocalEnvironment == nil {
		return
	}
	b.ensureInspection(selected.SessionID)
	if b.repo.busy {
		imgui.Text("Checking your local checkout...")
		return
	}
	if b.repo.err != "" {
		imgui.TextWrapped("Local checkout unknown: " + b.repo.err)
	} else if report, ok := b.alignmentFor(selected.SessionID); ok {
		if report.EnvironmentMatches {
			imgui.TextWrapped("Loaded environment hash matches the host.")
		} else {
			imgui.TextWrapped("Loaded environment hash differs from the host (yours " + ShortHash(b.repo.local.EnvironmentHash, 12) + "). The session cannot be opened until they match.")
		}
		switch report.Alignment.Status {
		case repoinfo.StatusUnknown:
			imgui.TextWrapped("Checkout: unknown. The loaded environment is not in a git worktree, git is unavailable, or the host published no commit.")
		default:
			imgui.TextWrapped("Checkout: " + report.Alignment.Status.String())
		}
		if report.Alignment.Dirty && report.Alignment.Status != repoinfo.StatusDirty {
			imgui.TextWrapped("The local worktree also has uncommitted changes.")
		}
		if text := report.Alignment.CopyText(); text != "" {
			imgui.TextWrapped("Run these in your checkout, then reload the environment and re-check. AphelionDMM never runs them for you.")
			imgui.TextWrapped(text)
			if b.CopyText != nil {
				w.Button("Copy git commands", func() { b.copyAlignment(selected.SessionID) }).Build()
				imgui.SameLine()
			}
		}
	}
	w.Disabled(b.repo.busy, w.Button("Re-check", b.Recheck)).Build()
}
