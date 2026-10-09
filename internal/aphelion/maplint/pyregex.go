package maplint

import (
	"regexp"
	"regexp/syntax"
	"strings"
	"unicode/utf8"
)

// pyRegexp is a compiled Python re.match pattern: anchored at the start, with
// no implicit end anchor. Most patterns compile directly to Go's RE2 engine.
// RE2 has no lookahead, so patterns that use negative lookahead groups `(?!X)`
// at the top level are evaluated by a small backtracking matcher (laAlt) that
// uses RE2 for every lookahead-free piece.
//
// Supported subset (everything else is reported as unsupported, never
// approximated):
//
//   - Any number of top-level negative lookaheads `(?!X)`, with top-level
//     alternation `|` between them. A lookahead inside a group, a repeated
//     lookahead, or a lookahead inside a lookahead is unsupported.
//   - X, and every piece between lookaheads, must be valid RE2 and must not
//     depend on text outside the piece: `\b`, `\B`, and `^`/`\A` anywhere but
//     the start of the first piece are rejected, as is `$` before the last
//     piece. `$` inside X is fine (it refers to the end of the subject).
//   - Positive lookahead, lookbehind, backreferences and inline flag groups
//     alongside a lookahead are unsupported.
//
// Known divergences shared with the rest of this package: Python's `$` also
// matches before a trailing newline, and `\s \w \d` follow RE2's ASCII rules.
type pyRegexp struct {
	re   *regexp.Regexp // set when the pattern has no top-level lookahead
	alts []*laAlt       // alternatives of a lookahead pattern
}

// MatchString reports whether re.match(pattern, s) would succeed.
func (p *pyRegexp) MatchString(s string) bool {
	if p.re != nil {
		return p.re.MatchString(s)
	}
	for _, alt := range p.alts {
		if alt.match(s) {
			return true
		}
	}
	return false
}

// laAlt is one top-level alternative `P0 (?!X0) P1 (?!X1) ... Pn`.
// pieces[k] for k < n must match exactly the text between two split points;
// the last piece only has to match a prefix of the remaining text.
type laAlt struct {
	pieces []*regexp.Regexp // \A(?:P)\z, except the last: \A(?:P)
	looks  []*regexp.Regexp // \A(?:X); a hit means the lookahead fails
}

// match tries every split position of every piece (a backtracking search made
// polynomial by remembering failed (piece, position) pairs).
func (a *laAlt) match(s string) bool {
	n := len(s)
	last := len(a.pieces) - 1
	failed := make([]bool, (last+1)*(n+1))
	var try func(k, pos int) bool
	try = func(k, pos int) bool {
		if k == last {
			return a.pieces[k].MatchString(s[pos:])
		}
		if failed[k*(n+1)+pos] {
			return false
		}
		for end := pos; end <= n; end++ {
			if end < n && !utf8.RuneStart(s[end]) {
				continue
			}
			if !a.pieces[k].MatchString(s[pos:end]) || a.looks[k].MatchString(s[end:]) {
				continue
			}
			if try(k+1, end) {
				return true
			}
		}
		failed[k*(n+1)+pos] = true
		return false
	}
	return try(0, 0)
}

type altParse struct {
	pieces []string
	looks  []string
}

// compilePython compiles a Python re.match pattern. Constructs outside the
// documented subset (positive lookahead, lookbehind, backreferences, nested
// lookahead, ...) are reported as unsupported.
func compilePython(p string) (*pyRegexp, error) {
	alts, found, err := splitLookaheads(p)
	if err != nil {
		return nil, unsupportedf("regular expression %q is not supported: %v", p, err)
	}
	if !found {
		if _, err := syntax.Parse(p, syntax.Perl); err != nil {
			return nil, unsupportedf("regular expression %q is not supported by the Go engine: %v", p, err)
		}
		re, err := regexp.Compile(`\A(?:` + p + `)`)
		if err != nil {
			return nil, unsupportedf("regular expression %q is not supported by the Go engine: %v", p, err)
		}
		return &pyRegexp{re: re}, nil
	}
	out := &pyRegexp{}
	for _, alt := range alts {
		compiled, err := compileAlt(alt)
		if err != nil {
			return nil, unsupportedf("regular expression %q is not supported: %v", p, err)
		}
		out.alts = append(out.alts, compiled)
	}
	return out, nil
}

func compileAlt(alt altParse) (*laAlt, error) {
	last := len(alt.pieces) - 1
	out := &laAlt{}
	for k, piece := range alt.pieces {
		c := contextTokens(piece)
		if c.boundary {
			return nil, errString("word boundaries next to a lookahead are not supported")
		}
		if (c.caret || c.startEsc) && k > 0 {
			return nil, errString("a start anchor after a lookahead is not supported")
		}
		if c.dollar && k < last {
			return nil, errString("an end anchor before a lookahead is not supported")
		}
		expr := `\A(?:` + piece + `)`
		if k < last {
			expr += `\z`
		}
		re, err := compileRE2(piece, expr)
		if err != nil {
			return nil, err
		}
		out.pieces = append(out.pieces, re)
	}
	for _, body := range alt.looks {
		c := contextTokens(body)
		if c.boundary || c.caret || c.startEsc {
			return nil, errString("anchors and word boundaries inside a lookahead are not supported")
		}
		re, err := compileRE2(body, `\A(?:`+body+`)`)
		if err != nil {
			return nil, err
		}
		out.looks = append(out.looks, re)
	}
	return out, nil
}

func compileRE2(raw, expr string) (*regexp.Regexp, error) {
	if _, err := syntax.Parse(raw, syntax.Perl); err != nil {
		return nil, err
	}
	return regexp.Compile(expr)
}

type errString string

func (e errString) Error() string { return string(e) }

// splitLookaheads splits p at its top-level `|` and `(?!X)` tokens. found is
// false when p has no top-level lookahead; the caller then uses plain RE2.
func splitLookaheads(p string) (alts []altParse, found bool, err error) {
	var cur altParse
	start, depth, flagGroup := 0, 0, false
	for i := 0; i < len(p); {
		switch p[i] {
		case '\\':
			i += 2
			continue
		case '[':
			end, ok := classEnd(p, i)
			if !ok {
				return nil, false, errString("unterminated character class")
			}
			i = end
			continue
		case '(':
			if strings.HasPrefix(p[i:], "(?") {
				rest := p[i+2:]
				switch {
				case strings.HasPrefix(rest, "="):
					return nil, false, errString("positive lookahead")
				case strings.HasPrefix(rest, "<="), strings.HasPrefix(rest, "<!"):
					return nil, false, errString("lookbehind")
				case strings.HasPrefix(rest, "!"):
					if depth > 0 {
						return nil, false, errString("lookahead inside a group")
					}
					end, ok := groupEnd(p, i)
					if !ok {
						return nil, false, errString("unterminated lookahead")
					}
					body := p[i+3 : end]
					if hasLookaround(body) {
						return nil, false, errString("nested lookaround")
					}
					if end+1 < len(p) && strings.IndexByte("*+?{", p[end+1]) >= 0 {
						return nil, false, errString("quantified lookahead")
					}
					cur.pieces = append(cur.pieces, p[start:i])
					cur.looks = append(cur.looks, body)
					found = true
					i = end + 1
					start = i
					continue
				case rest != "" && rest[0] != 'P' && rest[0] != ':' && rest[0] != '<' && (rest[0] == '-' || rest[0] >= 'a' && rest[0] <= 'z' || rest[0] >= 'A' && rest[0] <= 'Z'):
					flagGroup = true
				}
			}
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, false, errString("unbalanced parenthesis")
			}
		case '|':
			if depth == 0 {
				cur.pieces = append(cur.pieces, p[start:i])
				alts = append(alts, cur)
				cur = altParse{}
				start = i + 1
			}
		}
		i++
	}
	if depth != 0 {
		return nil, false, errString("unbalanced parenthesis")
	}
	if !found {
		return nil, false, nil
	}
	if flagGroup {
		return nil, false, errString("inline flags combined with a lookahead")
	}
	cur.pieces = append(cur.pieces, p[start:])
	return append(alts, cur), true, nil
}

// classEnd returns the index just past the character class that opens at i.
func classEnd(p string, i int) (int, bool) {
	j := i + 1
	if j < len(p) && p[j] == '^' {
		j++
	}
	if j < len(p) && p[j] == ']' {
		j++ // a leading ] is a literal
	}
	for j < len(p) {
		switch p[j] {
		case '\\':
			j += 2
			continue
		case ']':
			return j + 1, true
		}
		j++
	}
	return 0, false
}

// groupEnd returns the index of the ) that closes the group opened at i.
func groupEnd(p string, i int) (int, bool) {
	depth := 0
	for j := i; j < len(p); {
		switch p[j] {
		case '\\':
			j += 2
			continue
		case '[':
			end, ok := classEnd(p, j)
			if !ok {
				return 0, false
			}
			j = end
			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return j, true
			}
		}
		j++
	}
	return 0, false
}

// hasLookaround reports whether s contains any lookahead or lookbehind group.
func hasLookaround(s string) bool {
	for i := 0; i < len(s); {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '[':
			end, ok := classEnd(s, i)
			if !ok {
				return false
			}
			i = end
			continue
		case '(':
			rest := s[i+1:]
			for _, prefix := range [...]string{"?=", "?!", "?<=", "?<!"} {
				if strings.HasPrefix(rest, prefix) {
					return true
				}
			}
		}
		i++
	}
	return false
}

// tokens records the context-dependent constructs outside character classes.
type tokens struct {
	caret, dollar, boundary, startEsc bool
}

func contextTokens(s string) tokens {
	var t tokens
	for i := 0; i < len(s); {
		switch s[i] {
		case '\\':
			if i+1 < len(s) {
				switch s[i+1] {
				case 'b', 'B':
					t.boundary = true
				case 'A':
					t.startEsc = true
				case 'z', 'Z':
					t.dollar = true
				}
			}
			i += 2
			continue
		case '[':
			if end, ok := classEnd(s, i); ok {
				i = end
				continue
			}
		case '^':
			t.caret = true
		case '$':
			t.dollar = true
		}
		i++
	}
	return t
}
