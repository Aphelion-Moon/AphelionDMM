package dmmdata

import (
	"path/filepath"
	"strings"
	"testing"

	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRejectsUnterminatedDefinition(t *testing.T) {
	input := "//MAP CONVERTED BY dmm2tgm.py THIS HEADER COMMENT PREVENTS RECONVERSION, DO NOT REMOVE\n" +
		"\"a\" = (\n/obj/door{\n\tname = \"\n\t})\n\n(1,1,1) = {\"\na\n\"}\n"
	dmm, err := parse(&testReader{strings.NewReader(input)})
	require.Nil(t, dmm)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `key "a"`)
	assert.Contains(t, err.Error(), "not terminated")
}

func TestCheckVariableValue(t *testing.T) {
	valid := []string{
		`null`, `0`, `-1.5`, `"text"`, `""`, `"a \"quoted\" word"`, `"back\\slash"`,
		`'icons/obj/doors.dmi'`, `/obj/item/wrench`, `list("a" = 1, "b" = list(2,3))`,
		`list(1,2)`, `"semi; colon} brace"`, `"// not a comment"`,
	}
	for _, value := range valid {
		assert.NoError(t, CheckVariableValue("name", value), value)
	}

	invalid := map[string]string{
		`"`:               "unterminated",
		`"abc`:            "unterminated",
		`"abc\"`:          "unterminated",
		`1; x = 2`:        "splits into several variables",
		`5 + 3`:           "would be saved as",
		"\"line\nbreak\"": "would be saved as",
		`list(1}`:         "",
		`1 // note`:       "",
		`{"multi"}`:       "",
	}
	for value, want := range invalid {
		err := CheckVariableValue("name", value)
		require.Error(t, err, value)
		assert.Contains(t, err.Error(), want, value)
	}
}

func badValueMap() DmmData {
	good := dmmprefab.New(dmmprefab.IdNone, "/turf/floor", &dmvars.Variables{})
	bad := dmmprefab.New(dmmprefab.IdNone, "/obj/door", dmvars.Set(&dmvars.Variables{}, "name", `"`))
	return DmmData{
		IsTgm:      true,
		LineBreak:  "\n",
		KeyLength:  1,
		MaxX:       2,
		MaxY:       1,
		MaxZ:       1,
		Dictionary: DataDictionary{"a": Prefabs{good}, "b": Prefabs{good, bad}},
		Grid: DataGrid{
			util.Point{X: 1, Y: 1, Z: 1}: "a",
			util.Point{X: 2, Y: 1, Z: 1}: "b",
		},
	}
}

func TestValueProblemsNamesTheOffendingVariable(t *testing.T) {
	problems := badValueMap().ValueProblems(5)
	require.Len(t, problems, 1)
	assert.Equal(t, "/obj/door", problems[0].Path)
	assert.Equal(t, "name", problems[0].Name)
	assert.Equal(t, `"`, problems[0].Value)
	assert.Equal(t, util.Point{X: 2, Y: 1, Z: 1}, problems[0].Point)
}

func TestSaveWithInvalidValueExplainsTheCause(t *testing.T) {
	for _, tgm := range []bool{true, false} {
		data := badValueMap()
		data.IsTgm = tgm
		path := filepath.Join(t.TempDir(), "bad.dmm")
		data.Filepath = path
		err := data.Save()
		require.Error(t, err)
		assert.Contains(t, err.Error(), `/obj/door`)
		assert.Contains(t, err.Error(), `name = "`)
		assert.Contains(t, err.Error(), "(2,1,1)")
		assert.NotContains(t, err.Error(), "(0,0,0)")
		assert.NoFileExists(t, path)
	}
}
