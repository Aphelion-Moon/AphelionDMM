package dmmdata

import (
	"fmt"
	"os"
	"sort"
	// APHELION EDIT ADDITION START - DISK_VERSION
	"io"
	"sdmm/internal/aphelion/diskversion"
	// APHELION EDIT ADDITION END

	"sdmm/internal/util"
)

type (
	DataDictionary map[Key]Prefabs
	DataGrid       map[util.Point]Key
)

// DmmData stores raw information about the map. Mostly needed to for parsing and saving.
type DmmData struct {
	Filepath string
	// APHELION EDIT ADDITION START - DISK_VERSION
	DiskState diskversion.State
	// APHELION EDIT ADDITION END

	IsTgm     bool
	LineBreak string

	KeyLength        int
	MaxX, MaxY, MaxZ int

	Dictionary DataDictionary
	Grid       DataGrid
}

// APHELION EDIT CHANGE - ATOMIC_SAVE - ORIGINAL: func (d DmmData) Save()
func (d DmmData) Save() error {
	if d.IsTgm {
		return d.SaveTGM(d.Filepath)
	}
	return d.SaveDM(d.Filepath)
}

func (d DmmData) Keys() []Key {
	keys := make([]Key, 0, len(d.Dictionary))
	for key := range d.Dictionary {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool {
		return keys[i].ToNum() < keys[j].ToNum()
	})

	return keys
}

func (d DmmData) String() string {
	var winLineBreak bool
	if d.LineBreak == "\r\n" {
		winLineBreak = true
	}
	return fmt.Sprintf(
		"Filepath: %s, IsTgm: %t, WinLineBreak: %v, KeyLength: %d, MaxX: %d, MaxY: %d, MaxZ: %d",
		d.Filepath, d.IsTgm, winLineBreak, d.KeyLength, d.MaxX, d.MaxY, d.MaxZ)
}

func New(path string) (*DmmData, error) {
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	return NewWithSourceCopy(path, nil)
}

// NewWithSourceCopy copies the exact parsed bytes to an owned backup writer.
// The same content fingerprint and external-change check still qualify the load.
func NewWithSourceCopy(path string, copyTo io.Writer) (*DmmData, error) {
	// APHELION EDIT ADDITION END
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	// APHELION EDIT CHANGE - STATIC_ANALYSIS - ORIGINAL: defer file.Close()
	defer func() { _ = file.Close() }()
	// APHELION EDIT ADDITION START - DISK_VERSION
	// Original parser return is retained below; the reader now fingerprints
	// the same bytes consumed by the parser.
	/* APHELION EDIT REMOVAL START - DISK_VERSION
	return parse(file)
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	var input io.Reader = file
	if copyTo != nil {
		input = io.TeeReader(file, copyTo)
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - OWNED MAP OPEN - ORIGINAL: reader := diskversion.NewReader(file)
	reader := diskversion.NewReader(input)
	data, err := parse(namedVersionReader{Reader: reader, name: file.Name()})
	if err != nil {
		return nil, err
	}
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect loaded map %q: %w", path, err)
	}
	entryInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect loaded map entry %q: %w", path, err)
	}
	state, err := reader.State(fileInfo, entryInfo)
	if err != nil {
		return nil, err
	}
	if err := state.Check(path); err != nil {
		return nil, fmt.Errorf("map changed while loading: %w", err)
	}
	data.DiskState = state
	return data, nil
	// APHELION EDIT ADDITION END
}

// APHELION EDIT ADDITION START - DISK_VERSION
type namedVersionReader struct {
	io.Reader
	name string
}

func (reader namedVersionReader) Name() string { return reader.name }

// APHELION EDIT ADDITION END
