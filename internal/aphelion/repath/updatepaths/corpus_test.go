package updatepaths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRealScriptCorpus parses every script of a real codebase checkout:
// APHELION_UPDATEPATHS_CORPUS=<codebase>/tools/UpdatePaths/Scripts. Scripts are
// third-party content and are not vendored.
func TestRealScriptCorpus(t *testing.T) {
	dir := os.Getenv("APHELION_UPDATEPATHS_CORPUS")
	if dir == "" {
		t.Skip("set APHELION_UPDATEPATHS_CORPUS to a tools/UpdatePaths/Scripts directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var scripts []Script
	var errs []ParseError
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".txt") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		script, parseErrs := Parse(entry.Name(), data)
		scripts = append(scripts, script)
		errs = append(errs, parseErrs...)
		for _, rule := range script.Rules {
			reparsed, err := ParseRule(rule.String())
			if err != nil {
				t.Errorf("%s: formatted rule does not reparse: %v", rule.Pos, err)
				continue
			}
			if reparsed.String() != rule.String() {
				t.Errorf("%s: format is not stable: %q vs %q", rule.Pos, rule.String(), reparsed.String())
			}
		}
	}
	set := NewSet(scripts...)
	t.Logf("%d scripts, %d rules, %d rejected lines", len(scripts), set.Len(), len(errs))
	for _, err := range errs {
		t.Log(err)
	}
}
