package diskversion

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
)

var ErrConflict = errors.New("destination changed since it was observed")

// State records both the bytes read from a map and the filesystem entry that
// supplied them. A zero State represents an expected-missing destination.
type State struct {
	exists bool
	digest [sha256.Size]byte
	size   int64
	file   os.FileInfo
	entry  os.FileInfo
}

func Absent() State { return State{} }

func (state State) Exists() bool { return state.exists }

// Check verifies the destination immediately before a save replaces it.
func (state State) Check(path string) error {
	entry, err := os.Lstat(path)
	if !state.exists {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect save destination %q: %w", path, err)
		}
		return conflict(path, "a file appeared")
	}
	if errors.Is(err, os.ErrNotExist) {
		return conflict(path, "the file was deleted")
	}
	if err != nil {
		return fmt.Errorf("inspect save destination %q: %w", path, err)
	}
	if !os.SameFile(state.entry, entry) {
		return conflict(path, "the filesystem entry was replaced")
	}

	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return conflict(path, "the file was deleted")
	}
	if err != nil {
		return fmt.Errorf("open save destination %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect open save destination %q: %w", path, err)
	}
	if !os.SameFile(state.file, fileInfo) {
		return conflict(path, "the file contents were replaced")
	}

	reader := NewReader(file)
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("read save destination %q: %w", path, err)
	}
	if reader.size != state.size || reader.digest() != state.digest {
		return conflict(path, "the file contents changed")
	}

	fileInfoAfter, err := file.Stat()
	if err != nil {
		return fmt.Errorf("reinspect open save destination %q: %w", path, err)
	}
	entryAfter, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !os.SameFile(state.entry, entryAfter)) || !os.SameFile(state.file, fileInfoAfter) {
		return conflict(path, "the destination changed while it was checked")
	}
	if err != nil {
		return fmt.Errorf("reinspect save destination %q: %w", path, err)
	}
	return nil
}

// Capture reads a stable path into a State. A missing path produces Absent.
func Capture(path string) (State, error) {
	entryBefore, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Absent(), nil
	}
	if err != nil {
		return State{}, fmt.Errorf("inspect destination %q: %w", path, err)
	}

	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, conflict(path, "the file was deleted while its version was captured")
	}
	if err != nil {
		return State{}, fmt.Errorf("open destination %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	reader := NewReader(file)
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return State{}, fmt.Errorf("read destination %q: %w", path, err)
	}
	fileInfo, err := file.Stat()
	if err != nil {
		return State{}, fmt.Errorf("inspect open destination %q: %w", path, err)
	}
	if reader.size != fileInfo.Size() {
		return State{}, conflict(path, "the file changed while its version was captured")
	}
	entryAfter, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !os.SameFile(entryBefore, entryAfter)) {
		return State{}, conflict(path, "the filesystem entry changed while its version was captured")
	}
	if err != nil {
		return State{}, fmt.Errorf("reinspect destination %q: %w", path, err)
	}
	pathInfo, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !os.SameFile(fileInfo, pathInfo)) {
		return State{}, conflict(path, "the file changed while its version was captured")
	}
	if err != nil {
		return State{}, fmt.Errorf("reinspect destination target %q: %w", path, err)
	}
	state, err := reader.State(fileInfo, entryAfter)
	if err != nil {
		return State{}, err
	}
	if err := state.Check(path); err != nil {
		return State{}, err
	}
	return state, nil
}

func conflict(path, reason string) error {
	return fmt.Errorf("%w: %q (%s)", ErrConflict, path, reason)
}

type Reader struct {
	source io.Reader
	hash   hash.Hash
	size   int64
}

func NewReader(source io.Reader) *Reader {
	return &Reader{source: source, hash: sha256.New()}
}

func (reader *Reader) Read(buffer []byte) (int, error) {
	n, err := reader.source.Read(buffer)
	if n > 0 {
		_, _ = reader.hash.Write(buffer[:n])
		reader.size += int64(n)
	}
	return n, err
}

func (reader *Reader) digest() [sha256.Size]byte {
	var digest [sha256.Size]byte
	copy(digest[:], reader.hash.Sum(nil))
	return digest
}

func (reader *Reader) State(file, entry os.FileInfo) (State, error) {
	if file == nil || entry == nil {
		return State{}, fmt.Errorf("capture disk version: missing file identity")
	}
	if reader.size != file.Size() {
		return State{}, fmt.Errorf("capture disk version: read %d bytes, file size is %d", reader.size, file.Size())
	}
	return State{exists: true, digest: reader.digest(), size: reader.size, file: file, entry: entry}, nil
}

type Writer struct {
	destination io.Writer
	hash        hash.Hash
	size        int64
}

func NewWriter(destination io.Writer) *Writer {
	return &Writer{destination: destination, hash: sha256.New()}
}

func (writer *Writer) Write(buffer []byte) (int, error) {
	n, err := writer.destination.Write(buffer)
	if n > 0 {
		_, _ = writer.hash.Write(buffer[:n])
		writer.size += int64(n)
	}
	return n, err
}

func (writer *Writer) State(file, entry os.FileInfo) (State, error) {
	if file == nil || entry == nil {
		return State{}, fmt.Errorf("capture saved disk version: missing file identity")
	}
	if writer.size != file.Size() {
		return State{}, fmt.Errorf("capture saved disk version: wrote %d bytes, file size is %d", writer.size, file.Size())
	}
	var digest [sha256.Size]byte
	copy(digest[:], writer.hash.Sum(nil))
	return State{exists: true, digest: digest, size: writer.size, file: file, entry: entry}, nil
}
