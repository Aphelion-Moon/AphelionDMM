// Package updatepaths reads, evaluates and writes tgstation UpdatePaths
// scripts (tools/UpdatePaths). Evaluation follows the reference Python tool:
// rules apply in order and each rule sees the outputs of every earlier rule.
package updatepaths

import "fmt"

type Pos struct {
	File string
	Line int
}

func (p Pos) String() string { return fmt.Sprintf("%s:%d", p.File, p.Line) }

type FilterKind uint8

const (
	FilterEquals  FilterKind = iota // name = value
	FilterAny                       // name = @ANY: the variable is map-edited
	FilterUnset                     // name = @UNSET: the variable is not map-edited
	FilterNoEdits                   // {@UNSET}: the instance has no map edits at all
)

type Filter struct {
	Name  string
	Kind  FilterKind
	Value string
}

type Pattern struct {
	Path     string
	Subtypes bool // Path/@SUBTYPES matches Path and every descendant.
	Filters  []Filter
}

type PropKind uint8

const (
	PropLiteral PropKind = iota // name = value
	PropAllOld                  // @OLD: copy every old variable
	PropSkip                    // name = @SKIP
	PropOld                     // name = @OLD
	PropOldOf                   // name = @OLD:from
)

type Prop struct {
	Name  string
	Kind  PropKind
	Value string
	From  string
}

type OutputKind uint8

const (
	OutputPath     OutputKind = iota
	OutputKeepPath            // @OLD as the path keeps the matched path.
	OutputSubtypes            // Path/@SUBTYPES appends the matched subtype suffix.
)

type Output struct {
	Path  string
	Kind  OutputKind
	Props []Prop
}

type Rule struct {
	Match   Pattern
	Delete  bool
	Outputs []Output
	Pos     Pos
}

type Script struct {
	Name string
	// Order is the leading pull request number of the file name, or -1.
	Order int64
	Rules []Rule
}

type ParseError struct {
	Pos Pos
	Msg string
}

func (e ParseError) Error() string { return fmt.Sprintf("%s: %s", e.Pos, e.Msg) }

// Instance is one placed object: a type path and its map-edited variables.
// Values are raw DM literals and are compared as exact text.
type Instance struct {
	Path string
	Vars map[string]string
}
