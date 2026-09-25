package envsnapshot

import (
	"sdmm/third_party/sdmmparser"
	"testing"
)

func TestSnapshotRejectsDuplicatePathsAndInheritanceCycles(t *testing.T) {
	child := func(path, parent string) sdmmparser.ObjectTreeType {
		return sdmmparser.ObjectTreeType{Path: path, Vars: []sdmmparser.ObjectTreeVar{{Name: "parent_type", Value: parent}}}
	}
	for name, tree := range map[string]sdmmparser.ObjectTreeType{
		"duplicate":      {Children: []sdmmparser.ObjectTreeType{child("/one", "null"), child("/one", "null")}},
		"cycle":          {Children: []sdmmparser.ObjectTreeType{child("/one", "/two"), child("/two", "/one")}},
		"missing parent": {Children: []sdmmparser.ObjectTreeType{child("/one", "/absent")}},
	} {
		t.Run(name, func(t *testing.T) {
			if ValidateTree(&tree) == nil {
				t.Fatal("invalid references accepted")
			}
		})
	}
	valid := sdmmparser.ObjectTreeType{Children: []sdmmparser.ObjectTreeType{child("/one", "null"), child("/two", "/one")}}
	if err := ValidateTree(&valid); err != nil {
		t.Fatal(err)
	}
}
