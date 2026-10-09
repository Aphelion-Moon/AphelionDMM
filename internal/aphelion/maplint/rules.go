package maplint

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// UnsupportedError reports a rule (or part of one) whose semantics this
// evaluator does not implement. The rule is dropped, never approximated.
type UnsupportedError struct {
	File   string
	Rule   string // typepath key of the rule, empty for file-level problems
	Detail string
}

func (e *UnsupportedError) Error() string {
	if e.Rule != "" {
		return fmt.Sprintf("maplint: %s: rule %q unsupported: %s", e.File, e.Rule, e.Detail)
	}
	return fmt.Sprintf("maplint: %s: unsupported: %s", e.File, e.Detail)
}

// LoadError reports a file that could not be loaded at all (invalid YAML or
// invalid rule data that upstream maplint also rejects).
type LoadError struct {
	File string
	Err  error
}

func (e *LoadError) Error() string { return fmt.Sprintf("maplint: %s: %v", e.File, e.Err) }
func (e *LoadError) Unwrap() error { return e.Err }

// unsupportedDetail is the internal marker for "valid but not implemented".
type unsupportedDetail string

func (u unsupportedDetail) Error() string { return string(u) }

func unsupportedf(format string, args ...any) error {
	return unsupportedDetail(fmt.Sprintf(format, args...))
}

// ---- typepath matching (lint.py TypepathExtra) ----

type typepathExtra struct {
	wildcard bool
	exact    bool
	path     string
	segments []string
}

var typepathRe = regexp.MustCompile(`^/[\w/]+$`)

func newTypepathExtra(s string) (typepathExtra, error) {
	if s == "*" {
		return typepathExtra{wildcard: true}, nil
	}
	t := typepathExtra{}
	if strings.HasPrefix(s, "=") {
		t.exact = true
		s = s[1:]
	}
	if !typepathRe.MatchString(s) {
		return t, fmt.Errorf("invalid typepath %q", s)
	}
	t.path = s
	t.segments = strings.Split(s, "/")[1:]
	return t, nil
}

func splitPath(p string) []string {
	if !strings.HasPrefix(p, "/") {
		return nil
	}
	return strings.Split(p, "/")[1:]
}

// matches mirrors TypepathExtra.matches_path.
func (t *typepathExtra) matches(path string, segs []string) bool {
	if t.wildcard {
		return true
	}
	if t.exact {
		return t.path == path
	}
	if len(t.segments) > len(segs) {
		return false
	}
	for i, s := range t.segments {
		if segs[i] != s {
			return false
		}
	}
	return true
}

// ---- regex handling ----

// ---- neighbors (lint.py AtomNeighbor) ----

type neighbor struct {
	identical  bool
	tp         *typepathExtra
	pattern    *pyRegexp
	patternSrc string
}

func (n *neighbor) str() string {
	if n.tp != nil {
		return n.tp.path
	}
	if n.pattern != nil {
		return n.patternSrc
	}
	return "None"
}

// ---- banned variables (lint.py BannedVariable / extract_choices) ----

type choices struct {
	list    []constant // numbers (float) and strings
	pattern *pyRegexp
	src     string
	isList  bool
}

type bannedVariable struct {
	name  string
	allow *choices
	deny  *choices
}

// ---- when (lint.py When*) ----

type condKind int

const (
	condSet condKind = iota
	condNotSet
	condEqual
	condNotEqual
	condLike
)

type whenNode struct {
	// leaf
	leaf    bool
	text    string
	kind    condKind
	varName string
	value   string
	like    *pyRegexp
	// group
	all      bool
	children []*whenNode
}

var (
	reWhenSet      = regexp.MustCompile(`^(.+) is set`)
	reWhenNotSet   = regexp.MustCompile(`^(.+) is not set`)
	reWhenEqual    = regexp.MustCompile(`^(.+) is '(.+)'`)
	reWhenNotEqual = regexp.MustCompile(`^(.+) is not '(.+)'`)
	reWhenLike     = regexp.MustCompile(`^(.+) like '(.+)'`)
)

// matchString mirrors WhenCondition/WhenGroup.match_string.
func (w *whenNode) matchString(parentIntersection bool) string {
	if w.leaf {
		return w.text
	}
	sym := " or "
	if w.all {
		sym = " and "
	}
	parts := make([]string, len(w.children))
	for i, c := range w.children {
		parts[i] = c.matchString(w.all)
	}
	text := strings.Join(parts, sym)
	if !w.all && parentIntersection && len(w.children) > 1 {
		return "(" + text + ")"
	}
	return text
}

// ---- rule ----

type skipFile struct {
	substr string
	re     *regexp.Regexp
}

type rule struct {
	key             string
	tp              typepathExtra
	banned          bool
	bannedNeighbors []neighbor
	required        []neighbor
	ignored         []typepathExtra
	bannedVarsAll   bool
	bannedVars      []bannedVariable
	when            *whenNode // root group (always "all")
	skipFiles       []skipFile
}

// ---- YAML helpers ----

func nodeKind(n *yaml.Node) string {
	switch n.Kind {
	case yaml.MappingNode:
		return "mapping"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	}
	return "node"
}

// mapping checks for aliases, merge keys and duplicate keys (which Python's
// loader would resolve silently) and returns ordered key/value pairs.
type pair struct {
	key string
	k   *yaml.Node
	v   *yaml.Node
}

func readMapping(n *yaml.Node) ([]pair, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected a mapping, found %s", nodeKind(n))
	}
	seen := map[string]bool{}
	out := make([]pair, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			return nil, unsupportedf("non-scalar mapping key")
		}
		if k.ShortTag() == "!!merge" || k.Value == "<<" {
			return nil, unsupportedf("YAML merge keys")
		}
		if k.Kind == yaml.AliasNode || v.Kind == yaml.AliasNode {
			return nil, unsupportedf("YAML aliases")
		}
		if seen[k.Value] {
			return nil, unsupportedf("duplicate key %q", k.Value)
		}
		seen[k.Value] = true
		out = append(out, pair{k.Value, k, v})
	}
	return out, nil
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}

func asBool(n *yaml.Node, what string) (bool, error) {
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!bool" {
		return false, fmt.Errorf("%s must be a boolean", what)
	}
	var b bool
	if err := n.Decode(&b); err != nil {
		return false, fmt.Errorf("%s must be a boolean", what)
	}
	return b, nil
}

func asString(n *yaml.Node, what string) (string, error) {
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!str" {
		return "", fmt.Errorf("%s must be a string", what)
	}
	return n.Value, nil
}

func asStringList(n *yaml.Node, what string) ([]string, error) {
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%s must be a list", what)
	}
	out := make([]string, 0, len(n.Content))
	for _, c := range n.Content {
		s, err := asString(c, what+" entry")
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// ---- rule parsing (lint.py Rules.__init__) ----

func parseRule(key string, tp typepathExtra, n *yaml.Node) (*rule, error) {
	pairs, err := readMapping(n)
	if err != nil {
		if _, ok := err.(unsupportedDetail); ok {
			return nil, err
		}
		return nil, fmt.Errorf("lint rules must be a dictionary (%v)", err)
	}
	r := &rule{key: key, tp: tp}
	for _, p := range pairs {
		switch p.key {
		case "ignore":
			list, err := asStringList(p.v, "'ignore'")
			if err != nil {
				return nil, err
			}
			for _, s := range list {
				t, err := newTypepathExtra(s)
				if err != nil {
					return nil, err
				}
				r.ignored = append(r.ignored, t)
			}
		case "banned":
			if r.banned, err = asBool(p.v, "banned"); err != nil {
				return nil, err
			}
		case "banned_neighbors":
			if r.bannedNeighbors, err = parseNeighbors(p.v, "banned_neighbors"); err != nil {
				return nil, err
			}
		case "required_neighbors":
			if r.required, err = parseNeighbors(p.v, "required_neighbors"); err != nil {
				return nil, err
			}
		case "banned_variables":
			if err := parseBannedVariables(r, p.v); err != nil {
				return nil, err
			}
		case "when":
			if r.when, err = parseWhen(p.v); err != nil {
				return nil, err
			}
		case "skip_files":
			if r.skipFiles, err = parseSkipFiles(p.v); err != nil {
				return nil, err
			}
		default:
			return nil, unsupportedf("unknown rule key %q", p.key)
		}
	}
	return r, nil
}

func parseNeighbors(n *yaml.Node, what string) ([]neighbor, error) {
	switch n.Kind {
	case yaml.SequenceNode:
		out := make([]neighbor, 0, len(n.Content))
		for _, c := range n.Content {
			s, err := asString(c, what+" entry")
			if err != nil {
				return nil, err
			}
			nb, err := newNeighbor(s, nil)
			if err != nil {
				return nil, err
			}
			out = append(out, nb)
		}
		return out, nil
	case yaml.MappingNode:
		pairs, err := readMapping(n)
		if err != nil {
			return nil, err
		}
		out := make([]neighbor, 0, len(pairs))
		for _, p := range pairs {
			nb, err := newNeighbor(p.key, p.v)
			if err != nil {
				return nil, err
			}
			out = append(out, nb)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s must be a list, or a dictionary keyed by type", what)
}

// newNeighbor mirrors AtomNeighbor.__init__: keys with any lower-case letter
// are typepaths; fully upper-case keys are labels that need a pattern.
func newNeighbor(key string, data *yaml.Node) (neighbor, error) {
	var nb neighbor
	if strings.ToUpper(key) != key {
		t, err := newTypepathExtra(key)
		if err != nil {
			return nb, err
		}
		nb.tp = &t
	}
	if data == nil || isNull(data) {
		return nb, nil
	}
	pairs, err := readMapping(data)
	if err != nil {
		if _, ok := err.(unsupportedDetail); ok {
			return nb, err
		}
		return nb, fmt.Errorf("banned neighbor must be a dictionary")
	}
	for _, p := range pairs {
		switch p.key {
		case "identical":
			if nb.identical, err = asBool(p.v, "identical"); err != nil {
				return nb, err
			}
		case "pattern":
			s, err := asString(p.v, "pattern")
			if err != nil {
				return nb, err
			}
			if nb.pattern, err = compilePython(s); err != nil {
				return nb, err
			}
			nb.patternSrc = s
		case "ignore":
			// Parsed and validated upstream but never consulted by matches().
			list, err := asStringList(p.v, "ignore")
			if err != nil {
				return nb, err
			}
			for _, s := range list {
				if _, err := newTypepathExtra(s); err != nil {
					return nb, err
				}
			}
		default:
			return nb, unsupportedf("unknown key %q in banned neighbor", p.key)
		}
	}
	return nb, nil
}

func parseBannedVariables(r *rule, n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!bool" {
		b, _ := asBool(n, "banned_variables")
		if b {
			r.bannedVarsAll = true
			return nil
		}
		return unsupportedf("banned_variables: false (upstream asserts)")
	}
	switch n.Kind {
	case yaml.SequenceNode:
		for _, c := range n.Content {
			s, err := asString(c, "banned_variables entry")
			if err != nil {
				return unsupportedf("non-string banned_variables entry")
			}
			r.bannedVars = append(r.bannedVars, bannedVariable{name: s})
		}
		return nil
	case yaml.MappingNode:
		pairs, err := readMapping(n)
		if err != nil {
			return err
		}
		for _, p := range pairs {
			bv := bannedVariable{name: p.key}
			if !isNull(p.v) {
				sub, err := readMapping(p.v)
				if err != nil {
					if _, ok := err.(unsupportedDetail); ok {
						return err
					}
					return unsupportedf("banned variable %q: value must be empty or a mapping", p.key)
				}
				for _, s := range sub {
					switch s.key {
					case "allow":
						if bv.allow, err = parseChoices(s.v, "allow"); err != nil {
							return err
						}
					case "deny":
						if bv.deny, err = parseChoices(s.v, "deny"); err != nil {
							return err
						}
					default:
						return unsupportedf("unknown key %q in banned variable %s", s.key, p.key)
					}
				}
			}
			r.bannedVars = append(r.bannedVars, bv)
		}
		return nil
	}
	return fmt.Errorf("banned_variables must be a list, or a dictionary keyed by variable")
}

// parseChoices mirrors extract_choices. Upstream silently drops list entries
// that are not str/int/float; those are reported as unsupported here.
func parseChoices(n *yaml.Node, key string) (*choices, error) {
	switch n.Kind {
	case yaml.SequenceNode:
		c := &choices{isList: true}
		for _, item := range n.Content {
			if item.Kind != yaml.ScalarNode {
				return nil, unsupportedf("%s: nested value", key)
			}
			switch item.ShortTag() {
			case "!!str":
				c.list = append(c.list, constant{kind: constStr, s: item.Value, raw: item.Value})
			case "!!int", "!!float":
				var f float64
				if err := item.Decode(&f); err != nil {
					return nil, unsupportedf("%s: unreadable number %q", key, item.Value)
				}
				c.list = append(c.list, constant{kind: constNum, num: f})
			case "!!bool":
				b, _ := asBool(item, key)
				f := 0.0
				if b {
					f = 1.0 // Python bool is an int
				}
				c.list = append(c.list, constant{kind: constNum, num: f})
			default:
				return nil, unsupportedf("%s: unsupported %s entry", key, item.ShortTag())
			}
		}
		return c, nil
	case yaml.MappingNode:
		pairs, err := readMapping(n)
		if err != nil {
			return nil, err
		}
		for _, p := range pairs {
			if p.key == "pattern" {
				s, err := asString(p.v, "pattern")
				if err != nil {
					return nil, err
				}
				re, err := compilePython(s)
				if err != nil {
					return nil, err
				}
				return &choices{pattern: re, src: s}, nil
			}
		}
		return nil, fmt.Errorf("unknown key in %s", key)
	}
	return nil, fmt.Errorf("%s must be a list of constants, or a pattern", key)
}

func parseWhen(n *yaml.Node) (*whenNode, error) {
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("when must be a list of conditions")
	}
	root := &whenNode{all: true}
	for _, c := range n.Content {
		child, err := parseCondition(c)
		if err != nil {
			return nil, err
		}
		root.children = append(root.children, child)
	}
	return root, nil
}

func parseCondition(n *yaml.Node) (*whenNode, error) {
	switch n.Kind {
	case yaml.MappingNode:
		pairs, err := readMapping(n)
		if err != nil {
			return nil, err
		}
		if len(pairs) == 0 {
			return nil, fmt.Errorf("empty conditional group")
		}
		var chosen *pair
		all := true
		for i := range pairs {
			if pairs[i].key == "all" {
				chosen, all = &pairs[i], true
				break
			}
		}
		if chosen == nil {
			for i := range pairs {
				if pairs[i].key == "any" {
					chosen, all = &pairs[i], false
					break
				}
			}
		}
		if chosen == nil {
			return nil, fmt.Errorf("unknown conditional group in when clause: %s", pairs[0].key)
		}
		if chosen.v.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("conditional group %q must be a list", chosen.key)
		}
		g := &whenNode{all: all}
		for _, c := range chosen.v.Content {
			child, err := parseCondition(c)
			if err != nil {
				return nil, err
			}
			g.children = append(g.children, child)
		}
		return g, nil
	case yaml.ScalarNode:
		if n.ShortTag() != "!!str" {
			return nil, fmt.Errorf("invalid condition type: %s", n.ShortTag())
		}
		return parseWhenCondition(n.Value)
	}
	return nil, fmt.Errorf("invalid condition type: %s", nodeKind(n))
}

func parseWhenCondition(text string) (*whenNode, error) {
	w := &whenNode{leaf: true, text: text}
	type m struct {
		re   *regexp.Regexp
		kind condKind
	}
	matches := 0
	for _, c := range []m{{reWhenSet, condSet}, {reWhenNotSet, condNotSet}, {reWhenEqual, condEqual}, {reWhenNotEqual, condNotEqual}, {reWhenLike, condLike}} {
		g := c.re.FindStringSubmatch(text)
		if g == nil {
			continue
		}
		matches++
		w.kind = c.kind
		w.varName = g[1]
		if len(g) > 2 {
			w.value = g[2]
		}
	}
	if matches != 1 {
		return nil, fmt.Errorf("conditional rule must be either is set, is not set, is 'value', is not 'value', or like 'regex'; found: %s", text)
	}
	if w.kind == condLike {
		re, err := compilePython(w.value)
		if err != nil {
			return nil, err
		}
		w.like = re
	}
	return w, nil
}

func parseSkipFiles(n *yaml.Node) ([]skipFile, error) {
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("skip_files must be a list")
	}
	var out []skipFile
	for _, c := range n.Content {
		switch c.Kind {
		case yaml.ScalarNode:
			s, err := asString(c, "skip_files entry")
			if err != nil {
				return nil, err
			}
			out = append(out, skipFile{substr: s})
		case yaml.MappingNode:
			pairs, err := readMapping(c)
			if err != nil {
				return nil, err
			}
			var sf skipFile
			found := false
			for _, p := range pairs {
				if p.key != "pattern" {
					return nil, fmt.Errorf("unknown key in skip_files entry: %s", p.key)
				}
				s, err := asString(p.v, "pattern")
				if err != nil {
					return nil, err
				}
				// Upstream uses re.search here: unanchored.
				if _, perr := syntax.Parse(s, syntax.Perl); perr != nil {
					return nil, unsupportedf("regular expression %q is not supported by the Go engine: %v", s, perr)
				}
				sf.re, err = regexp.Compile(s)
				if err != nil {
					return nil, unsupportedf("regular expression %q is not supported by the Go engine: %v", s, err)
				}
				found = true
			}
			if !found {
				return nil, fmt.Errorf("skip_files entries must be strings or dicts with a 'pattern' key")
			}
			out = append(out, sf)
		default:
			return nil, fmt.Errorf("skip_files entries must be strings or dicts with a 'pattern' key")
		}
	}
	return out, nil
}

var _ = strconv.Itoa
