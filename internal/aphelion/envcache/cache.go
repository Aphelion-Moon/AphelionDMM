// Package envcache stores local parser output, never an authoritative map or a
// trusted environment fingerprint. Every hit revalidates consumed bytes/probes.
package envcache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sdmm/internal/aphelion/configstore"
	"sdmm/internal/aphelion/resources"
)

const FormatVersion = 2
const MaxEntryBytes int64 = 256 << 20
const maxInputs = 100000

type Access struct {
	Kind         string `json:"kind"`
	Path         string `json:"path"`
	ResolvedPath string `json:"resolved_path"`
	SHA256       string `json:"sha256,omitempty"`
}
type Probe struct {
	Kind         string `json:"kind"`
	Path         string `json:"path"`
	ResolvedPath string `json:"resolved_path"`
	Exists       bool   `json:"exists"`
}
type Trace struct {
	SchemaVersion uint32   `json:"schema_version"`
	Complete      bool     `json:"complete"`
	WorkingDir    string   `json:"working_dir"`
	ConfigMode    string   `json:"config_mode"`
	Accesses      []Access `json:"accesses"`
	Probes        []Probe  `json:"probes"`
}
type envelope struct {
	Version                                    int
	Magic                                      string
	Length                                     int
	Root, Parser, Platform, WorkingDir, Digest string
	Trace                                      Trace
	Tree                                       json.RawMessage
}
type Entry struct {
	Tree  json.RawMessage
	Trace Trace
	lease *resources.Reservation
}

func (e *Entry) Close() {
	if e != nil {
		e.lease.Release()
	}
}

type Cache struct {
	Directory                   string
	Hits, Misses, Invalidations atomic.Uint64
}

func New(directory string) *Cache { return &Cache{Directory: directory} }

var writers sync.Mutex
var ErrMiss = errors.New("environment cache miss")
var ErrInputsChanged = errors.New("parser inputs changed")

func ValidateInputs(ctx context.Context, root string, trace Trace) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return validate(ctx, root, cwd, trace)
}

// This detects corruption, not an attacker with the user's write authority.
// The entire record is covered, including the validation manifest.
func checksum(record envelope) string {
	record.Digest = ""
	h := sha256.New()
	encoder := json.NewEncoder(h)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(record) != nil {
		return ""
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func key(root, parser, cwd string) string {
	return fmt.Sprintf("%x.json", sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%s|%s/%s", FormatVersion, root, parser, cwd, runtime.GOOS, runtime.GOARCH))))
}
func (c *Cache) Load(ctx context.Context, root, parser string) (entry *Entry, resultErr error) {
	defer func() {
		if resultErr != nil {
			c.Misses.Add(1)
		} else {
			c.Hits.Add(1)
		}
	}()
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err = privateDirectory(c.Directory); err != nil {
		return nil, err
	}
	filePath := filepath.Join(c.Directory, key(root, parser, cwd))
	if err = privateStorage(filePath, false); err != nil {
		return nil, err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, ErrMiss
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() <= 0 || info.Size() > MaxEntryBytes {
		return nil, fmt.Errorf("%w: entry size", ErrMiss)
	}
	lease, err := resources.DefaultBudget().Reserve(uint64(info.Size()) * 8)
	if err != nil {
		return nil, err
	}
	accepted := false
	defer func() {
		if !accepted {
			lease.Release()
		}
	}()
	input, err := io.ReadAll(io.LimitReader(f, info.Size()+1))
	if err != nil || int64(len(input)) != info.Size() || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: read", ErrMiss)
	}
	var stored envelope
	if err = json.Unmarshal(input, &stored); err != nil {
		return nil, fmt.Errorf("%w: corrupt envelope", ErrMiss)
	}
	if stored.Magic != "APHELION-DME" || stored.Length != len(stored.Tree) || stored.Version != FormatVersion || stored.Root != root || stored.Parser != parser || stored.WorkingDir != cwd || stored.Platform != runtime.GOOS+"/"+runtime.GOARCH || stored.Digest != checksum(stored) || !json.Valid(stored.Tree) {
		c.Invalidations.Add(1)
		return nil, fmt.Errorf("%w: incompatible or corrupt payload", ErrMiss)
	}
	if err = validate(ctx, root, cwd, stored.Trace); err != nil {
		c.Invalidations.Add(1)
		return nil, fmt.Errorf("%w: %v", ErrMiss, err)
	}
	accepted = true
	return &Entry{Tree: stored.Tree, Trace: stored.Trace, lease: lease}, nil
}

func (c *Cache) Put(ctx context.Context, root, parser string, tree []byte, trace Trace) error {
	if int64(len(tree)) > MaxEntryBytes/2 || len(trace.Accesses)+len(trace.Probes) > maxInputs {
		return fmt.Errorf("cache payload is invalid or oversized")
	}
	lease, err := resources.DefaultBudget().Reserve(uint64(len(tree))*5 + uint64(len(trace.Accesses)+len(trace.Probes))*2048)
	if err != nil {
		return err
	}
	defer lease.Release()
	var canonical bytes.Buffer
	if err := json.Compact(&canonical, tree); err != nil {
		return err
	}
	tree = canonical.Bytes()
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err = validate(ctx, root, cwd, trace); err != nil {
		return err
	}
	stored := envelope{Version: FormatVersion, Magic: "APHELION-DME", Length: len(tree), Root: root, Parser: parser, Platform: runtime.GOOS + "/" + runtime.GOARCH, WorkingDir: cwd, Trace: trace, Tree: tree}
	stored.Digest = checksum(stored)
	var encodedBuffer bytes.Buffer
	encoder := json.NewEncoder(&encodedBuffer)
	encoder.SetEscapeHTML(false)
	if err = encoder.Encode(stored); err != nil {
		return err
	}
	encoded := encodedBuffer.Bytes()
	if int64(len(encoded)) > MaxEntryBytes {
		return fmt.Errorf("cache entry exceeds byte limit")
	}
	writers.Lock()
	defer writers.Unlock()
	if err = os.MkdirAll(c.Directory, 0700); err != nil {
		return err
	}
	if err = privateDirectory(c.Directory); err != nil {
		return err
	}
	unlock, err := storageLock(filepath.Join(c.Directory, ".writer.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	if err = c.prune(); err != nil {
		return err
	}
	file, err := os.CreateTemp(c.Directory, ".envcache-*.tmp")
	if err != nil {
		return err
	}
	stage := file.Name()
	defer func() { _ = os.Remove(stage) }()
	if _, err = file.Write(encoded); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = configstore.PublishStaged(stage, filepath.Join(c.Directory, key(root, parser, cwd))); err != nil {
		return err
	}
	return c.prune()
}

func (c *Cache) prune() error {
	entries, err := cacheDirectoryEntries(c.Directory)
	if err != nil {
		return err
	}
	var files []os.FileInfo
	var size int64
	var activeBytes int64
	for _, entry := range entries {
		if entry.IsDir() || (!cacheFilename(entry.Name()) && !stageFilename(entry.Name())) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if stageFilename(entry.Name()) {
			if time.Since(info.ModTime()) > 24*time.Hour {
				if err = os.Remove(filepath.Join(c.Directory, entry.Name())); err != nil {
					return err
				}
			} else {
				activeBytes += info.Size()
			}
			continue
		}
		files = append(files, info)
		size += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime().Before(files[j].ModTime()) })
	for len(files) > 0 && (len(files) > 8 || size+activeBytes > (1<<30)-MaxEntryBytes) {
		file := files[0]
		files = files[1:]
		if err = os.Remove(filepath.Join(c.Directory, file.Name())); err != nil {
			return err
		}
		size -= file.Size()
	}
	if size+activeBytes > (1<<30)-MaxEntryBytes {
		return fmt.Errorf("cache storage occupied by active or recent writers")
	}
	return nil
}

func cacheDirectoryEntries(path string) ([]os.DirEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	entries, err := f.ReadDir(257)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 256 {
		return nil, fmt.Errorf("cache directory scan limit reached")
	}
	return entries, nil
}
func stageFilename(name string) bool {
	if !strings.HasPrefix(name, ".envcache-") || !strings.HasSuffix(name, ".tmp") {
		return false
	}
	suffix := strings.TrimSuffix(strings.TrimPrefix(name, ".envcache-"), ".tmp")
	if len(suffix) == 0 || len(suffix) > 20 {
		return false
	}
	for _, digit := range suffix {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
func cacheFilename(name string) bool {
	if len(name) != 69 || !strings.HasSuffix(name, ".json") {
		return false
	}
	for _, ch := range name[:64] {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

// Clear removes only this format's owned content-addressed entry names.
func (c *Cache) Clear() error {
	writers.Lock()
	defer writers.Unlock()
	if err := privateDirectory(c.Directory); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	unlock, err := storageLock(filepath.Join(c.Directory, ".writer.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	entries, err := cacheDirectoryEntries(c.Directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		info, infoErr := entry.Info()
		abandoned := infoErr == nil && stageFilename(entry.Name()) && time.Since(info.ModTime()) > 24*time.Hour
		if !entry.IsDir() && (cacheFilename(entry.Name()) || abandoned) {
			if err = os.Remove(filepath.Join(c.Directory, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func validate(ctx context.Context, root, cwd string, trace Trace) error {
	if !trace.Complete || trace.SchemaVersion != 1 || trace.ConfigMode != "default-no-autodetect" || trace.WorkingDir != cwd || len(trace.Accesses) == 0 || len(trace.Accesses)+len(trace.Probes) > maxInputs {
		return fmt.Errorf("incomplete or incompatible dependency trace")
	}
	rootSeen := false
	var consumed int64
	observed := make(map[string]os.FileInfo)
	paths := newPathValidator(cwd, filepath.Dir(root))
	digests := make(map[string]string)
	for _, access := range trace.Accesses {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := paths.check(access.Path, access.ResolvedPath, false); err != nil {
			return err
		}
		if access.Kind != "read" && access.Kind != "open_only" {
			return fmt.Errorf("unknown access kind")
		}
		if access.Kind == "read" {
			digest, ok := digests[access.ResolvedPath]
			if !ok {
				f, err := os.Open(access.ResolvedPath)
				if err != nil {
					return err
				}
				info, err := f.Stat()
				if err != nil || !info.Mode().IsRegular() || info.Size() > 128<<20 || consumed+info.Size() > 2<<30 {
					_ = f.Close()
					return fmt.Errorf("dependency exceeds aggregate read budget or is not a regular file")
				}
				h := sha256.New()
				n, err := io.Copy(h, io.LimitReader(contextReader{ctx, f}, info.Size()+1))
				_ = f.Close()
				if err != nil {
					return err
				}
				if n != info.Size() {
					return fmt.Errorf("%w during validation", ErrInputsChanged)
				}
				consumed += n
				observed[access.ResolvedPath] = info
				digest = fmt.Sprintf("%x", h.Sum(nil))
				digests[access.ResolvedPath] = digest
			}
			if digest != access.SHA256 {
				return fmt.Errorf("%w: %s", ErrInputsChanged, access.Path)
			}
			if sameLexical(access.ResolvedPath, root) {
				rootSeen = true
			}
		} else {
			f, err := os.Open(access.ResolvedPath)
			if err != nil {
				return err
			}
			_ = f.Close()
		}
	}
	if !rootSeen {
		return fmt.Errorf("trace omits consumed root")
	}
	for _, probe := range trace.Probes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if probe.Kind != "include_candidate" && probe.Kind != "f_exists" {
			return fmt.Errorf("unknown probe kind")
		}
		// Existing parser fexists semantics can resolve relative to its launch
		// directory. Only that caller-selected directory and project are eligible.
		if err := paths.check(probe.Path, probe.ResolvedPath, true); err != nil {
			return err
		}
		_, err := os.Stat(probe.ResolvedPath)
		if (err == nil) != probe.Exists {
			return fmt.Errorf("%w: existence probe %s", ErrInputsChanged, probe.Path)
		}
	}
	for path, before := range observed {
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			return fmt.Errorf("%w during validation: %s", ErrInputsChanged, path)
		}
	}
	return ctx.Err()
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func sameLexical(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func privateDirectory(directory string) error {
	root, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	directory, err = filepath.Abs(directory)
	if err != nil || !contained(root, directory) {
		return fmt.Errorf("environment cache must be inside local user cache storage")
	}
	for {
		if err := privateStorage(directory, true); err != nil {
			return err
		}
		if sameLexical(directory, root) {
			return nil
		}
		directory = filepath.Dir(directory)
	}
}
func contained(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
