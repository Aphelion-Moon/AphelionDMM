package dmenv

import (
	// APHELION EDIT ADDITION START - ENVIRONMENT SNAPSHOT
	"context"
	"sdmm/internal/aphelion/envsnapshot"
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - ENVIRONMENT GENERATION FINGERPRINT
	"sdmm/internal/aphelion/envload"
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - UI STAGE TRACE
	"sdmm/internal/aphelion/diagnostics/uistage"
	// APHELION EDIT ADDITION END
	"path/filepath"
	"sdmm/third_party/sdmmparser"
	"strings"

	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmvars"
)

type Dme struct {
	Name     string
	RootDir  string
	RootFile string
	Objects  map[string]*Object
	// APHELION EDIT ADDITION START - ENVIRONMENT SNAPSHOT
	CacheStatus string
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - ENVIRONMENT GENERATION FINGERPRINT
	// Set only after native reconstruction and parent linkage. Parsed environments
	// are read-only after publication; manually assembled fixtures remain uncached.
	fingerprint string
	// APHELION EDIT ADDITION END
}

func New(path string) (*Dme, error) {
	// APHELION EDIT ADDITION START - ENVIRONMENT SNAPSHOT
	return NewWithOptions(context.Background(), path, envsnapshot.Options{})
}

func NewWithOptions(ctx context.Context, path string, options envsnapshot.Options) (*Dme, error) {
	return NewWithProgress(ctx, path, options, nil)
}

func NewWithProgress(ctx context.Context, path string, options envsnapshot.Options, progress func(string)) (*Dme, error) {
	stage := func(name string) {
		if progress != nil {
			progress(name)
		}
	}
	stage("Validating cache / parsing environment")
	// APHELION EDIT ADDITION END
	dme := Dme{
		Name:     filepath.Base(path),
		RootDir:  filepath.Dir(path),
		RootFile: path,
		Objects:  make(map[string]*Object),
	}

	// APHELION EDIT ADDITION START - UI STAGE TRACE
	parse := uistage.Begin(uistage.EnvironmentParse)
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - ENVIRONMENT SNAPSHOT - ORIGINAL: objectTreeType, err := sdmmparser.ParseEnvironment(path)
	loaded, err := envsnapshot.Load(ctx, path, options)
	// APHELION EDIT ADDITION START - UI STAGE TRACE
	parse.End()
	// APHELION EDIT ADDITION END
	if err != nil {
		return nil, err
	}
	// APHELION EDIT ADDITION START - ENVIRONMENT SNAPSHOT
	defer loaded.Close()
	objectTreeType := loaded.Tree
	dme.CacheStatus = loaded.Status
	// APHELION EDIT ADDITION END

	// APHELION EDIT ADDITION START - UI STAGE TRACE
	reconstruct := uistage.Begin(uistage.EnvironmentReconstruct)
	stage("Reconstructing and linking objects")
	// APHELION EDIT ADDITION END
	traverseTree0(objectTreeType, "", nil, &dme)

	for _, object := range dme.Objects {
		if parentType, ok := object.Vars.Value("parent_type"); ok {
			object.parent = dme.Objects[parentType]
		}
		if object.parent != nil {
			object.Vars.LinkParent(object.parent.Vars)
		}
	}

	// APHELION EDIT ADDITION START - ENVIRONMENT GENERATION FINGERPRINT
	reconstruct.End()
	fingerprint := uistage.Begin(uistage.EnvironmentFingerprint)
	stage("Fingerprinting environment")
	dme.fingerprint, err = dme.EnvironmentHash()
	fingerprint.End()
	if err != nil {
		return nil, err
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - ENVIRONMENT SNAPSHOT
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	loaded.Persist()
	stage("Waiting for UI installation")
	// APHELION EDIT ADDITION END
	return &dme, nil
}

// APHELION EDIT ADDITION START - ENVIRONMENT GENERATION FINGERPRINT
func (d *Dme) EnvironmentHash() (string, error) {
	if d.fingerprint != "" {
		return d.fingerprint, nil
	}
	objects := make(map[string]*dmvars.Variables, len(d.Objects))
	for path, object := range d.Objects {
		if object != nil {
			objects[path] = object.Vars
		} else {
			objects[path] = nil
		}
	}
	return envload.Fingerprint(objects)
}

// APHELION EDIT ADDITION END

func nameFromPath(path string, parentName string) string {
	if parentName == "" && len(path) > 1 {
		return "\"" + dm.PathLast(path) + "\""
	}
	return parentName
}

func traverseTree0(root *sdmmparser.ObjectTreeType, parentName string, parent *Object, dme *Dme) {
	variables := dmvars.MutableVariables{}
	varFlags := make(map[string]VarFlags, len(root.Vars))
	var name string

	for _, treeVar := range root.Vars {
		value := sanitizeVar(treeVar.Value)

		if treeVar.Name == "name" {
			if value == dmvars.NullValue {
				value = nameFromPath(root.Path, parentName)
			}

			name = value
		}

		variables.Put(treeVar.Name, value)

		if treeVar.Decl {
			var flags VarFlags
			flags.Tmp = treeVar.IsTmp
			flags.Const = treeVar.IsConst
			flags.Static = treeVar.IsStatic
			varFlags[treeVar.Name] = flags
		}
	}

	if _, ok := variables.Value("name"); !ok {
		variables.Put("name", nameFromPath(root.Path, parentName))
	}

	object := &Object{
		env:      dme,
		parent:   parent,
		Path:     root.Path,
		Vars:     variables.ToImmutable(),
		VarFlags: varFlags,
		Location: root.Location,
	}

	children := make([]string, 0, len(root.Children))
	for _, child := range root.Children {
		children = append(children, child.Path)
		traverseTree0(&child, name, object, dme)
	}

	object.DirectChildren = children
	dme.Objects[root.Path] = object
}

func sanitizeVar(value string) string {
	if len(value) > 2 && strings.HasPrefix(value, "{\"") && strings.HasSuffix(value, "\"}") {
		value = value[1 : len(value)-1]
		return value
	}
	return value
}
