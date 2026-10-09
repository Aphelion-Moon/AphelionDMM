package ui

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/repoinfo"
)

func hostDescriptor() protocol.RepositoryDescriptor {
	return protocol.RepositoryDescriptor{DMEName: "game.dme", EnvironmentHash: strings.Repeat("a", 64), GitBranch: "play-test", GitCommit: strings.Repeat("1", 40)}
}

func TestBuildAlignmentReport(t *testing.T) {
	host := hostDescriptor()
	local := LocalEnvironment{DMEName: "game.dme", EnvironmentHash: host.EnvironmentHash, Repository: repoinfo.Local{Repository: true, Branch: "play-test", Commit: host.GitCommit}}
	if report := BuildAlignmentReport(host, local); !report.EnvironmentMatches || report.Alignment.Status != repoinfo.StatusMatches {
		t.Fatalf("matching checkout = %+v", report)
	}
	local.Repository.Commit = strings.Repeat("2", 40)
	local.EnvironmentHash = strings.Repeat("b", 64)
	report := BuildAlignmentReport(host, local)
	if report.EnvironmentMatches || report.Alignment.Status != repoinfo.StatusDifferentCommit || !strings.Contains(report.Alignment.CopyText(), "git switch --detach "+host.GitCommit) {
		t.Fatalf("different checkout = %+v", report)
	}
	host.GitBranch = "--evil"
	if report := BuildAlignmentReport(host, local); report.Alignment.Status != repoinfo.StatusUnknown || report.Alignment.CopyText() != "" {
		t.Fatalf("hostile descriptor produced commands: %+v", report)
	}
}

func TestDescribeRepositoryHandlesUnknown(t *testing.T) {
	if got := DescribeRepository(nil); !strings.Contains(got, "unknown") {
		t.Fatal(got)
	}
	host := hostDescriptor()
	text := DescribeRepository(&host)
	if !strings.Contains(text, "game.dme") || !strings.Contains(text, "play-test") || strings.Contains(text, host.GitCommit) {
		t.Fatalf("description = %q", text)
	}
}

func TestBrowserInspectsLocalCheckoutOnSelectionAndRecheck(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	native := imgui.CreateContext(nil)
	defer native.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 1000, Y: 700})
	io.SetDeltaTime(1.0 / 60)
	io.Fonts().TextureDataRGBA32()

	host := hostDescriptor()
	client := NewSessionClient(SessionClientConfig{})
	client.hostedBaseURL = "https://maps.example"
	client.hostedCredential = "secret"
	client.hostedCredentialExpires = time.Now().Add(time.Hour)
	inspections := 0
	local := LocalEnvironment{DMEName: "game.dme", EnvironmentHash: host.EnvironmentHash, Repository: repoinfo.Local{Repository: true, Branch: "main", Commit: strings.Repeat("2", 40), Dirty: true}}
	var copied string
	browser := &Browser{
		Client: client, Schedule: func(f func()) { f() }, scope: "community",
		LocalEnvironment: func() func(context.Context) (LocalEnvironment, error) {
			return func(context.Context) (LocalEnvironment, error) { inspections++; return local, nil }
		},
		CopyText: func(text string) { copied = text },
		Run:      func(f func()) { f() },
	}
	browser.account = client.HostedAccount()
	browser.page = protocol.HostedSessionsPage{Sessions: []protocol.HostedSessionSummary{{SessionID: "s", Available: true, Repository: &host}}}
	browser.selected = "s"
	frame := func() {
		imgui.NewFrame()
		imgui.Begin("Browser fixture")
		browser.Process()
		imgui.End()
		imgui.Render()
	}
	frame()
	frame()
	if inspections != 1 {
		t.Fatalf("selection inspected %d times; rendering must not re-run git", inspections)
	}
	report, ok := browser.alignmentFor("s")
	if !ok || report.Alignment.Status != repoinfo.StatusDifferentCommit || !report.Alignment.Dirty {
		t.Fatalf("report = %+v ok=%v", report, ok)
	}
	browser.copyAlignment("s")
	if !strings.Contains(copied, "git fetch") || !strings.Contains(copied, "git switch --detach "+host.GitCommit) || !strings.HasPrefix(copied, "# ") {
		t.Fatalf("copied = %q", copied)
	}
	// The user aligns and reloads the environment; re-check reflects it.
	local.Repository.Commit, local.Repository.Branch, local.Repository.Dirty = host.GitCommit, host.GitBranch, false
	browser.Recheck()
	frame()
	if inspections != 2 {
		t.Fatalf("recheck inspected %d times", inspections)
	}
	if report, _ := browser.alignmentFor("s"); report.Alignment.Status != repoinfo.StatusMatches {
		t.Fatalf("after alignment status = %v", report.Alignment.Status)
	}
	// Selecting another session discards the previous comparison.
	browser.selected = "other"
	if _, ok := browser.alignmentFor("other"); ok {
		t.Fatal("comparison leaked across sessions")
	}
}
