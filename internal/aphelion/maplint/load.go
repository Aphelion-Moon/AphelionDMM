// Package maplint evaluates the mechanical per-tile rules of a DM repository's
// tools/maplint/lints/*.yml files. The rules are data owned by that
// repository; they are read at runtime and never executed as code. The
// semantics follow the repository's own maplint (source/lint.py); anything not
// implemented is reported as unsupported instead of being guessed.
package maplint

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	maxRuleFileBytes = 1 << 20
	maxRuleFiles     = 1024
)

type lintFile struct {
	name  string
	help  string
	rules []*rule
}

type ruleRef struct {
	rule    *rule
	file    *lintFile
	fileIdx int
	ruleIdx int
}

// ruleIndex maps type path prefixes to rules so a tile check only visits
// rules that can match an atom's path.
type ruleIndex struct {
	prefix   map[string][]ruleRef // "/obj/structure" -> subtype rules
	exact    map[string][]ruleRef // "/obj/item" -> "=" rules
	wildcard []ruleRef
}

func (ix *ruleIndex) add(ref ruleRef) {
	t := &ref.rule.tp
	switch {
	case t.wildcard:
		ix.wildcard = append(ix.wildcard, ref)
	case t.exact:
		ix.exact[t.path] = append(ix.exact[t.path], ref)
	default:
		k := "/" + strings.Join(t.segments, "/")
		ix.prefix[k] = append(ix.prefix[k], ref)
	}
}

func (ix *ruleIndex) candidates(segs []string, path string, dst []ruleRef) []ruleRef {
	dst = append(dst, ix.wildcard...)
	dst = append(dst, ix.exact[path]...)
	if len(ix.prefix) > 0 {
		n := 0
		for i := range segs {
			n += len(segs[i]) + 1
			// "/" + join(segs[:i+1]) is path[:n] for paths of the form "/a/b".
			if n <= len(path) {
				dst = append(dst, ix.prefix[path[:n]]...)
			}
		}
	}
	return dst
}

// RuleSet is an immutable, concurrency-safe set of loaded rules.
type RuleSet struct {
	files       []*lintFile
	idx         ruleIndex
	unsupported []*UnsupportedError
}

// Files returns the base names of the files that contributed at least one rule or loaded cleanly.
func (rs *RuleSet) Files() []string {
	out := make([]string, len(rs.files))
	for i, f := range rs.files {
		out[i] = f.name
	}
	return out
}

// RuleCount returns the number of loaded (supported) rules.
func (rs *RuleSet) RuleCount() int {
	n := 0
	for _, f := range rs.files {
		n += len(f.rules)
	}
	return n
}

// Unsupported returns the rules that were dropped because their semantics are not implemented.
func (rs *RuleSet) Unsupported() []*UnsupportedError {
	return append([]*UnsupportedError(nil), rs.unsupported...)
}

// FindLintDir returns <dir of the .dme>/tools/maplint/lints when it exists.
// If environmentPath is itself a directory it is used as the repository root.
func FindLintDir(environmentPath string) (string, bool) {
	if environmentPath == "" {
		return "", false
	}
	root := filepath.Dir(environmentPath)
	if fi, err := os.Stat(environmentPath); err == nil && fi.IsDir() {
		root = environmentPath
	}
	dir := filepath.Join(root, "tools", "maplint", "lints")
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, true
	}
	return "", false
}

// Load parses every *.yml file in dir. A file that cannot be loaded yields a
// *LoadError and contributes nothing; a rule whose semantics are unsupported
// yields an *UnsupportedError and is dropped while the file's other rules still
// load. The returned RuleSet is never nil.
func Load(dir string) (*RuleSet, []error) {
	rs := &RuleSet{idx: ruleIndex{prefix: map[string][]ruleRef{}, exact: map[string][]ruleRef{}}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return rs, []error{&LoadError{File: dir, Err: err}}
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var errs []error
	if len(names) > maxRuleFiles {
		errs = append(errs, &LoadError{File: dir, Err: fmt.Errorf("too many rule files (%d, limit %d)", len(names), maxRuleFiles)})
		names = names[:maxRuleFiles]
	}
	for _, name := range names {
		lf, fileErrs := loadFile(filepath.Join(dir, name), name)
		for _, e := range fileErrs {
			var u *UnsupportedError
			if errors.As(e, &u) {
				rs.unsupported = append(rs.unsupported, u)
			}
		}
		errs = append(errs, fileErrs...)
		if lf == nil {
			continue
		}
		fileIdx := len(rs.files)
		rs.files = append(rs.files, lf)
		for ri, r := range lf.rules {
			rs.idx.add(ruleRef{rule: r, file: lf, fileIdx: fileIdx, ruleIdx: ri})
		}
	}
	return rs, errs
}

func loadFile(path, name string) (*lintFile, []error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, []error{&LoadError{name, err}}
	}
	defer func() { _ = f.Close() }() // Read-only; a close failure cannot lose data.
	data, err := io.ReadAll(io.LimitReader(f, maxRuleFileBytes+1))
	if err != nil {
		return nil, []error{&LoadError{name, err}}
	}
	if len(data) > maxRuleFileBytes {
		return nil, []error{&LoadError{name, fmt.Errorf("rule file exceeds %d bytes", maxRuleFileBytes)}}
	}
	return parseFile(name, data)
}

func parseFile(name string, data []byte) (*lintFile, []error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, []error{&LoadError{name, err}}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, []error{&LoadError{name, errors.New("lint must be a dictionary")}}
	}
	pairs, err := readMapping(doc.Content[0])
	if err != nil {
		if _, ok := err.(unsupportedDetail); ok {
			return nil, []error{&UnsupportedError{File: name, Detail: err.Error()}}
		}
		return nil, []error{&LoadError{name, errors.New("lint must be a dictionary")}}
	}
	lf := &lintFile{name: name}
	type pending struct {
		key string
		tp  typepathExtra
		val *yaml.Node
	}
	var todo []pending
	// Validate everything that makes upstream reject the whole file first.
	for _, p := range pairs {
		if p.key == "help" {
			if isNull(p.v) {
				continue
			}
			if lf.help, err = asString(p.v, "lint help"); err != nil {
				return nil, []error{&LoadError{name, err}}
			}
			continue
		}
		tp, err := newTypepathExtra(p.key)
		if err != nil {
			return nil, []error{&LoadError{name, err}}
		}
		todo = append(todo, pending{p.key, tp, p.v})
	}
	var errs []error
	var rules []*rule
	for _, t := range todo {
		r, err := parseRule(t.key, t.tp, t.val)
		if err == nil {
			rules = append(rules, r)
			continue
		}
		if u, ok := err.(unsupportedDetail); ok {
			errs = append(errs, &UnsupportedError{File: name, Rule: t.key, Detail: string(u)})
			continue
		}
		// Upstream rejects the whole file for invalid rule data.
		return nil, []error{&LoadError{name, fmt.Errorf("rule %q: %w", t.key, err)}}
	}
	lf.rules = rules
	return lf, errs
}
