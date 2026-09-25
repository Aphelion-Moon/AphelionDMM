// Package stamps stores reusable mechanical selections, without source paths or
// instance identities. The normal placement engine assigns identities per use.
package stamps

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmclip"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type prefab struct {
	Path string            `json:"path"`
	Vars map[string]string `json:"vars"`
}
type tile struct {
	X       int      `json:"x"`
	Y       int      `json:"y"`
	Prefabs []prefab `json:"prefabs"`
}
type document struct {
	Format          string   `json:"format"`
	Version         int      `json:"version"`
	Name            string   `json:"name"`
	EnvironmentHash string   `json:"environment_hash"`
	Width           int      `json:"width"`
	Height          int      `json:"height"`
	HiddenPaths     []string `json:"hidden_paths"`
	Tiles           []tile   `json:"tiles"`
}

// Stamp owns its data. Public access never returns aliases to the saved values.
type Stamp struct {
	data        document
	preview     string
	mu          sync.Mutex
	reservation *resources.Reservation
	readers     int
	closed      bool
}

func (s *Stamp) Name() string {
	if s == nil || s.isClosed() {
		return ""
	}
	return s.data.Name
}

func (s *Stamp) EnvironmentHash() string {
	if s == nil || s.isClosed() {
		return ""
	}
	return s.data.EnvironmentHash
}

// ReadLease pins immutable stamp data while a worker builds its placement
// source, even if the UI replaces or closes the owning stamp in the meantime.
type ReadLease struct {
	stamp    *Stamp
	mu       sync.Mutex
	released bool
}

func (s *Stamp) Acquire() (*ReadLease, error) {
	if s == nil {
		return nil, fmt.Errorf("stamp is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("stamp is closed")
	}
	s.readers++
	return &ReadLease{stamp: s}, nil
}

func (s *Stamp) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Stamp) releaseReader() {
	s.mu.Lock()
	s.readers--
	var reservation *resources.Reservation
	if s.closed && s.readers == 0 {
		reservation = s.reservation
		s.reservation = nil
	}
	s.mu.Unlock()
	reservation.Release()
}

func (r *ReadLease) Release() {
	if r == nil || r.stamp == nil {
		return
	}
	r.mu.Lock()
	if r.released {
		r.mu.Unlock()
		return
	}
	r.released = true
	r.mu.Unlock()
	r.stamp.releaseReader()
}

func Capture(name, environmentHash string, m *dmmap.Dmm, coords []util.Point, filter *dm.PathsFilter) (*Stamp, error) {
	return CaptureWithBudget(name, environmentHash, m, coords, filter, resources.DefaultBudget())
}

// CaptureWithBudget admits its retained document and preview memory from an
// injectable byte budget before materializing the stamp.
func CaptureWithBudget(name, environmentHash string, m *dmmap.Dmm, coords []util.Point, filter *dm.PathsFilter, budget *resources.Budget) (*Stamp, error) {
	if m == nil || filter == nil || len(coords) == 0 {
		return nil, fmt.Errorf("select tiles on a valid map for a stamp")
	}
	needed, err := estimateCaptureMemory(m, coords, filter)
	if err != nil {
		return nil, err
	}
	reservation, err := budgetOrDefault(budget).Reserve(needed)
	if err != nil {
		return nil, err
	}
	d := document{Format: "aphelion-selection-stamp", Version: 1, Name: strings.TrimSpace(name), EnvironmentHash: environmentHash, HiddenPaths: filter.HiddenPaths()}
	minX, minY := coords[0].X, coords[0].Y
	for _, c := range coords {
		minX, minY = min(minX, c.X), min(minY, c.Y)
	}
	for _, c := range coords {
		if !m.HasTile(c) || c.Z != coords[0].Z {
			reservation.Release()
			return nil, fmt.Errorf("stamp tiles must be inside the map on one level")
		}
		source := m.GetTile(c)
		if source == nil {
			reservation.Release()
			return nil, fmt.Errorf("stamp source tile is missing")
		}
		t := tile{X: c.X - minX + 1, Y: c.Y - minY + 1}
		d.Width, d.Height = max(d.Width, t.X), max(d.Height, t.Y)
		for _, i := range source.Instances() {
			if i == nil || i.Prefab() == nil || i.Prefab().Vars() == nil {
				reservation.Release()
				return nil, fmt.Errorf("stamp source contains a damaged prefab")
			}
			if !filter.IsVisiblePath(i.Prefab().Path()) {
				continue
			}
			p := prefab{Path: i.Prefab().Path(), Vars: make(map[string]string)}
			for _, name := range i.Prefab().Vars().Iterate() {
				value, ok := i.Prefab().Vars().Value(name)
				if !ok {
					reservation.Release()
					return nil, fmt.Errorf("stamp variable has no value")
				}
				p.Vars[name] = value
			}
			t.Prefabs = append(t.Prefabs, p)
		}
		d.Tiles = append(d.Tiles, t)
	}
	sort.Slice(d.Tiles, func(i, j int) bool {
		if d.Tiles[i].Y != d.Tiles[j].Y {
			return d.Tiles[i].Y < d.Tiles[j].Y
		}
		return d.Tiles[i].X < d.Tiles[j].X
	})
	s := &Stamp{data: d, reservation: reservation}
	if err := s.data.validate(); err != nil {
		reservation.Release()
		return nil, err
	}
	s.preview = s.buildPreview()
	return s, nil
}

func estimateCaptureMemory(m *dmmap.Dmm, coords []util.Point, filter *dm.PathsFilter) (uint64, error) {
	bytes := uint64(1 << 20)
	for _, coord := range coords {
		if !m.HasTile(coord) || coord.Z != coords[0].Z {
			return 0, fmt.Errorf("stamp tiles must be inside the map on one level")
		}
		tile := m.GetTile(coord)
		if tile == nil {
			return 0, fmt.Errorf("stamp source tile is missing")
		}
		bytes = stampAdd(bytes, 256)
		for _, instance := range tile.Instances() {
			if instance == nil || instance.Prefab() == nil || instance.Prefab().Vars() == nil {
				return 0, fmt.Errorf("stamp source contains a damaged prefab")
			}
			prefab := instance.Prefab()
			if !filter.IsVisiblePath(prefab.Path()) {
				continue
			}
			bytes = stampAdd(bytes, 160+uint64(len(prefab.Path())))
			for _, name := range prefab.Vars().Iterate() {
				value, ok := prefab.Vars().Value(name)
				if !ok {
					return 0, fmt.Errorf("stamp variable has no value")
				}
				bytes = stampAdd(bytes, 128+uint64(len(name))+uint64(len(value)))
			}
		}
	}
	return stampAdd(bytes, stampMul(bytes, 2)), nil
}

func (d document) validate() error {
	if d.Format != "aphelion-selection-stamp" || d.Version != 1 {
		return fmt.Errorf("unsupported selection stamp format or version")
	}
	if !utf8.ValidString(d.Name) || strings.TrimSpace(d.Name) == "" || utf8.RuneCountInString(d.Name) > 128 || strings.ContainsFunc(d.Name, unicode.IsControl) {
		return fmt.Errorf("stamp name must contain 1 through 128 printable characters")
	}
	// Empty provenance is retained for legacy/external clipboard data. It never
	// matches a loaded environment and requires explicit placement acknowledgement.
	if d.EnvironmentHash != "" {
		if err := model.ValidateSHA256("stamp environment hash", d.EnvironmentHash); err != nil {
			return err
		}
	}
	if d.Width < 1 || d.Height < 1 || d.Width > model.MaxMapDimension || d.Height > model.MaxMapDimension || len(d.Tiles) == 0 || len(d.Tiles) > d.Width*d.Height {
		return fmt.Errorf("stamp dimensions or tile count exceed supported limits")
	}
	seen := make(map[[2]int]bool)
	minX, minY, maxX, maxY := d.Width, d.Height, 0, 0
	for _, t := range d.Tiles {
		coord := [2]int{t.X, t.Y}
		if t.X < 1 || t.Y < 1 || t.X > d.Width || t.Y > d.Height || seen[coord] {
			return fmt.Errorf("stamp has an out-of-bounds or duplicate tile")
		}
		seen[coord] = true
		minX, minY, maxX, maxY = min(minX, t.X), min(minY, t.Y), max(maxX, t.X), max(maxY, t.Y)
		for _, p := range t.Prefabs {
			if !validPath(p.Path) || p.Vars == nil {
				return fmt.Errorf("stamp has an invalid prefab")
			}
			for name, value := range p.Vars {
				if name == "" || !utf8.ValidString(name) || strings.ContainsRune(name, 0) || !utf8.ValidString(value) {
					return fmt.Errorf("stamp has invalid variable data")
				}
			}
		}
	}
	if minX != 1 || minY != 1 || maxX != d.Width || maxY != d.Height {
		return fmt.Errorf("stamp dimensions must match normalized tile bounds")
	}
	hidden := make(map[string]bool)
	for _, path := range d.HiddenPaths {
		if !validPath(path) || hidden[path] {
			return fmt.Errorf("stamp has an invalid or duplicate hidden path")
		}
		hidden[path] = true
	}
	return nil
}

func validPath(path string) bool {
	return strings.HasPrefix(path, "/") && utf8.ValidString(path) && !strings.ContainsRune(path, 0)
}

func (s *Stamp) encodeTo(w io.Writer) error {
	if s.isClosed() {
		return fmt.Errorf("stamp is closed")
	}
	if err := s.data.validate(); err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(s.data)
}

func (s *Stamp) Save(path string) error {
	if s == nil || s.isClosed() {
		return fmt.Errorf("stamp is closed")
	}
	if !strings.EqualFold(filepath.Ext(path), ".admmstamp") {
		return fmt.Errorf("choose an .admmstamp file")
	}
	var expected [32]byte
	err := dmmdata.SaveAtomic(path, func(w io.Writer) error {
		digest := sha256.New()
		if err := s.encodeTo(io.MultiWriter(w, digest)); err != nil {
			return err
		}
		copy(expected[:], digest.Sum(nil))
		return nil
	}, func(staged string) error {
		file, err := os.Open(staged)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		actual := sha256.New()
		if _, err := io.Copy(actual, file); err != nil {
			return err
		}
		if !equalDigest(expected, actual) {
			return fmt.Errorf("staged stamp differs from captured selection")
		}
		return nil
	})
	return err
}

func Load(path string) (*Stamp, error) {
	return LoadWithBudget(path, resources.DefaultBudget())
}

// LoadWithBudget decodes and validates a stamp as a bounded stream. File size
// is used only to reserve expected decoded memory, not as a product-size cap.
func LoadWithBudget(path string, budget *resources.Budget) (*Stamp, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 {
		return nil, fmt.Errorf("choose a regular stamp file")
	}
	needed := estimateLoadMemory(uint64(info.Size()))
	reservation, err := budgetOrDefault(budget).Reserve(needed)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		reservation.Release()
		return nil, err
	}
	defer func() { _ = f.Close() }()
	limit := info.Size()
	if limit < int64(^uint64(0)>>1) {
		limit++
	}
	counted := &countingReader{reader: io.LimitReader(f, limit)}
	reader := newStrictUTF8Reader(counted)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var d document
	if err := decoder.Decode(&d); err != nil {
		reservation.Release()
		return nil, fmt.Errorf("read stamp: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		reservation.Release()
		if err != nil {
			return nil, fmt.Errorf("stamp contains trailing or invalid data: %w", err)
		}
		return nil, fmt.Errorf("stamp contains trailing data")
	}
	if counted.count != info.Size() {
		reservation.Release()
		return nil, fmt.Errorf("stamp file changed while it was being read")
	}
	if err := d.validate(); err != nil {
		reservation.Release()
		return nil, err
	}
	s := &Stamp{data: d, reservation: reservation}
	s.preview = s.buildPreview()
	return s, nil
}

// Close releases the stamp's admitted memory when its owner replaces or drops
// it. A closed stamp can no longer be saved or placed.
func (s *Stamp) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	var reservation *resources.Reservation
	if s.readers == 0 {
		reservation = s.reservation
		s.reservation = nil
	}
	s.mu.Unlock()
	reservation.Release()
}

// PasteData preserves captured and currently hidden paths. Prefabs acquire the
// current environment's defaults; unknown paths retain their explicit values.
func (s *Stamp) PasteData(current *dm.PathsFilter, environment *dmenv.Dme) dmmclip.PasteData {
	lease, err := s.Acquire()
	if err != nil {
		return dmmclip.PasteData{}
	}
	defer lease.Release()
	data, reservation, err := lease.PasteData(context.Background(), current, environment, resources.DefaultBudget())
	if err != nil {
		return dmmclip.PasteData{}
	}
	reservation.Release()
	return data
}

// TileCount reports captured coordinate count without materializing paste tiles.
func (s *Stamp) TileCount() int {
	if s == nil || s.isClosed() {
		return 0
	}
	return len(s.data.Tiles)
}

// TileCount reports the immutable tile count pinned by a live read lease. It
// remains available if the owner closes the stamp after acquiring the lease.
func (r *ReadLease) TileCount() int {
	if r == nil || r.stamp == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.released {
		return 0
	}
	return len(r.stamp.data.Tiles)
}

// PasteData builds a detached template on a worker and retains a reservation
// for the returned tile data until the caller releases it.
func (r *ReadLease) PasteData(ctx context.Context, current *dm.PathsFilter, environment *dmenv.Dme, budget *resources.Budget) (dmmclip.PasteData, *resources.Reservation, error) {
	if r == nil || r.stamp == nil {
		return dmmclip.PasteData{}, nil, fmt.Errorf("stamp read lease is unavailable")
	}
	r.mu.Lock()
	if r.released {
		r.mu.Unlock()
		return dmmclip.PasteData{}, nil, fmt.Errorf("stamp read lease has been released")
	}
	r.stamp.mu.Lock()
	r.stamp.readers++
	r.stamp.mu.Unlock()
	r.mu.Unlock()
	defer r.stamp.releaseReader()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return dmmclip.PasteData{}, nil, err
	}
	reservation, err := budgetOrDefault(budget).Reserve(estimatePasteDataMemory(r.stamp.data))
	if err != nil {
		return dmmclip.PasteData{}, nil, err
	}
	data, err := buildPasteData(ctx, r.stamp.data, current, environment)
	if err != nil {
		reservation.Release()
		return dmmclip.PasteData{}, nil, err
	}
	return data, reservation, nil
}

func buildPasteData(ctx context.Context, document document, current *dm.PathsFilter, environment *dmenv.Dme) (dmmclip.PasteData, error) {
	filter := dm.NewPathsFilterEmpty()
	var currentHidden []string
	if current != nil {
		currentHidden = current.HiddenPaths()
	}
	for _, path := range append(currentHidden, document.HiddenPaths...) {
		if filter.IsVisiblePath(path) {
			filter.TogglePath(path)
		}
	}
	data := dmmclip.PasteData{Filter: filter.Copy()}
	for _, saved := range document.Tiles {
		if err := ctx.Err(); err != nil {
			return dmmclip.PasteData{}, err
		}
		t := dmmap.Tile{Coord: util.Point{X: saved.X, Y: saved.Y, Z: 1}}
		for _, p := range saved.Prefabs {
			if err := ctx.Err(); err != nil {
				return dmmclip.PasteData{}, err
			}
			vars := &dmvars.MutableVariables{}
			for _, name := range variableNames(p.Vars) {
				vars.Put(name, p.Vars[name])
			}
			values := vars.ToImmutable()
			if environment != nil {
				if object := environment.Objects[p.Path]; object != nil {
					values.LinkParent(object.Vars)
				}
			}
			t.InstancesAdd(dmmprefab.New(0, p.Path, values))
		}
		data.Buffer = append(data.Buffer, t)
	}
	return data, nil
}

func estimatePasteDataMemory(document document) uint64 {
	bytes := uint64(1<<20) + stampMul(uint64(len(document.Tiles)), 256)
	for _, tile := range document.Tiles {
		for _, prefab := range tile.Prefabs {
			bytes = stampAdd(bytes, 160+uint64(len(prefab.Path)))
			for name, value := range prefab.Vars {
				bytes = stampAdd(bytes, 128+uint64(len(name))+uint64(len(value)))
			}
		}
	}
	return stampAdd(bytes, stampMul(bytes, 2))
}

func variableNames(vars map[string]string) []string {
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *Stamp) Preview() string {
	if s == nil || s.isClosed() {
		return ""
	}
	return s.preview
}

func (s *Stamp) buildPreview() string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n%d x %d; %d selected tiles; %d captured hidden paths.\n", s.Name(), s.data.Width, s.data.Height, len(s.data.Tiles), len(s.data.HiddenPaths))
	for _, t := range s.data.Tiles[:min(len(s.data.Tiles), 12)] {
		fmt.Fprintf(&out, "\n(%d, %d): %d prefabs\n", t.X, t.Y, len(t.Prefabs))
		for _, p := range t.Prefabs[:min(len(t.Prefabs), 8)] {
			fmt.Fprintln(&out, previewText(p.Path))
			names := variableNames(p.Vars)
			for _, name := range names[:min(len(names), 8)] {
				fmt.Fprintf(&out, "  %s = %s\n", previewText(name), previewText(p.Vars[name]))
			}
			if len(names) > 8 {
				fmt.Fprintln(&out, "  More variables in stamp file.")
			}
		}
		if len(t.Prefabs) > 8 {
			fmt.Fprintln(&out, "More prefabs in stamp file.")
		}
	}
	if len(s.data.Tiles) > 12 {
		fmt.Fprintln(&out, "More tiles in stamp file.")
	}
	return out.String()
}

func previewText(value string) string {
	if len(value) > 256 {
		end := 256
		for !utf8.RuneStart(value[end]) {
			end--
		}
		value = value[:end] + "… [truncated]"
	}
	return strconv.Quote(value)
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.count += int64(n)
	return n, err
}

type strictUTF8Reader struct {
	reader  *bufio.Reader
	pending []byte
	offset  int
	err     error
}

func newStrictUTF8Reader(reader io.Reader) *strictUTF8Reader {
	return &strictUTF8Reader{reader: bufio.NewReaderSize(reader, 32<<10)}
}

func (r *strictUTF8Reader) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	written := 0
	for written < len(dst) {
		if r.offset < len(r.pending) {
			n := copy(dst[written:], r.pending[r.offset:])
			written += n
			r.offset += n
			continue
		}
		if r.err != nil {
			if written != 0 {
				return written, nil
			}
			return 0, r.err
		}
		runeValue, size, err := r.reader.ReadRune()
		if err != nil {
			r.err = err
			continue
		}
		if runeValue == utf8.RuneError && size == 1 {
			r.err = fmt.Errorf("stamp contains invalid UTF-8")
			continue
		}
		r.pending = utf8.AppendRune(r.pending[:0], runeValue)
		r.offset = 0
	}
	return written, nil
}

func estimateLoadMemory(fileBytes uint64) uint64 {
	maxUint := ^uint64(0)
	overhead := uint64(1 << 20)
	if fileBytes > (maxUint-overhead)/12 {
		return ^uint64(0)
	}
	return fileBytes*12 + overhead
}

func stampAdd(left, right uint64) uint64 {
	if right > ^uint64(0)-left {
		return ^uint64(0)
	}
	return left + right
}

func stampMul(left, right uint64) uint64 {
	if left != 0 && right > ^uint64(0)/left {
		return ^uint64(0)
	}
	return left * right
}

func budgetOrDefault(budget *resources.Budget) *resources.Budget {
	if budget == nil {
		return resources.DefaultBudget()
	}
	return budget
}

func equalDigest(expected [32]byte, actual hash.Hash) bool {
	return subtle.ConstantTimeCompare(expected[:], actual.Sum(nil)) == 1
}
