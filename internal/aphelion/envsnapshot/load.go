package envsnapshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"sdmm/internal/aphelion/diagnostics/uistage"
	"sdmm/internal/aphelion/envcache"
	"sdmm/internal/aphelion/resources"
	"sdmm/third_party/sdmmparser"
)

// Change when parser hooks, default options, builtins or exported semantics change.
const ParserIdentity = "spacemandmm-0290db5-trace2-sdmmparser-2.0-export1"

type Options struct {
	Enabled, Rebuild bool
	Directory        string
}
type Result struct {
	Tree           *sdmmparser.ObjectTreeType
	Status         string
	cache          *envcache.Cache
	root           string
	parserIdentity string
	raw            json.RawMessage
	trace          envcache.Trace
	lease          *resources.Reservation
	entry          *envcache.Entry
}

func (r *Result) Close() {
	if r != nil {
		r.lease.Release()
		r.lease = nil
		r.entry.Close()
		r.entry = nil
	}
}

func DefaultDirectory() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "AphelionDMM", "environment-snapshots-v2"), nil
}

var writerBusy atomic.Bool

// Persist is called after reconstruction succeeded. One bounded writer may own
// the existing raw bytes; competing or memory-constrained writes are skipped.
func (r *Result) Persist() {
	if r.cache == nil || len(r.raw) == 0 || !writerBusy.CompareAndSwap(false, true) {
		return
	}
	cache, root, parserIdentity, raw, trace, lease := r.cache, r.root, r.parserIdentity, r.raw, r.trace, r.lease
	r.lease = nil
	r.raw = nil
	go func() {
		defer writerBusy.Store(false)
		defer lease.Release()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		stage := uistage.Begin("aphelion.environment.cache_write")
		defer stage.End()
		if err := cache.Put(ctx, root, parserIdentity, raw, trace); err != nil {
			log.Debug().Err(err).Msg("environment cache write skipped")
		}
	}()
}

func Load(ctx context.Context, path string, options Options) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !options.Enabled {
		tree, err := sdmmparser.ParseEnvironment(path)
		return &Result{Tree: tree, Status: "cache bypassed"}, err
	}
	root, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	directory := options.Directory
	if directory == "" {
		directory, err = DefaultDirectory()
	}
	cache := envcache.New(directory)
	parserIdentity := ParserIdentity
	if !filepath.IsAbs(path) {
		// Native source locations preserve the caller's root spelling. A
		// relative invocation cannot reuse an absolute invocation's export.
		parserIdentity += "|relative-root:" + path
	}
	status := "cache miss"
	if err == nil && !options.Rebuild {
		stage := uistage.Begin("aphelion.environment.cache_validate_restore")
		entry, loadErr := cache.Load(ctx, root, parserIdentity)
		if loadErr == nil {
			var tree sdmmparser.ObjectTreeType
			loadErr = json.Unmarshal(entry.Tree, &tree)
			if loadErr == nil {
				loadErr = ValidateTree(&tree)
			}
			if loadErr == nil && ctx.Err() == nil {
				stage.End()
				return &Result{Tree: &tree, Status: "validated cache hit", entry: entry}, nil
			}
			entry.Close()
		}
		stage.End()
		status = fmt.Sprintf("cache miss: %v", loadErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fallback := func() (*Result, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tree, err := sdmmparser.ParseEnvironment(path)
		if err == nil {
			err = ValidateTree(tree)
		}
		if err == nil {
			err = ctx.Err()
		}
		return &Result{Tree: tree, Status: "cache bypassed: trace transfer limit"}, err
	}
	data, lease, err := sdmmparser.ParseEnvironmentTraced(path)
	if errors.Is(err, sdmmparser.ErrTraceTransferLimit) {
		return fallback()
	}
	if err != nil {
		return nil, err
	}
	result := &Result{lease: lease, root: root, parserIdentity: parserIdentity, Status: status}
	var traced struct {
		Tree  json.RawMessage `json:"object_tree"`
		Trace envcache.Trace  `json:"input_trace"`
	}
	if err = json.Unmarshal(data, &traced); err != nil {
		result.Close()
		return nil, err
	}
	var tree sdmmparser.ObjectTreeType
	if err = json.Unmarshal(traced.Tree, &tree); err != nil {
		result.Close()
		return nil, err
	}
	if err = ValidateTree(&tree); err != nil {
		result.Close()
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		result.Close()
		return nil, err
	}
	result.Tree = &tree
	if traced.Trace.Complete && directory != "" {
		// Observed parse-time drift is not published. Retry once from native.
		if err = envcache.ValidateInputs(ctx, root, traced.Trace); err != nil {
			if cancelled := ctx.Err(); cancelled != nil {
				result.Close()
				return nil, cancelled
			}
			if !errors.Is(err, envcache.ErrInputsChanged) {
				result.Status += "; source is uncacheable: " + err.Error()
				return result, nil
			}
			result.Close()
			data, lease, err = sdmmparser.ParseEnvironmentTraced(path)
			if errors.Is(err, sdmmparser.ErrTraceTransferLimit) {
				return fallback()
			}
			if err != nil {
				return nil, err
			}
			result.lease = lease
			if err = json.Unmarshal(data, &traced); err == nil {
				err = json.Unmarshal(traced.Tree, &tree)
			}
			if err == nil {
				err = ValidateTree(&tree)
			}
			if err == nil {
				err = ctx.Err()
			}
			if err != nil {
				result.Close()
				return nil, err
			}
			if err == nil && traced.Trace.Complete {
				err = envcache.ValidateInputs(ctx, root, traced.Trace)
			}
			if errors.Is(err, envcache.ErrInputsChanged) {
				result.Close()
				return nil, fmt.Errorf("environment inputs changed repeatedly; retry after source edits settle: %w", err)
			}
			if err != nil {
				if cancelled := ctx.Err(); cancelled != nil {
					result.Close()
					return nil, cancelled
				}
				result.Status += "; source is uncacheable: " + err.Error()
				return result, nil
			}
		}
		if !traced.Trace.Complete {
			result.Status += "; parser dependency trace is incomplete"
			return result, nil
		}
		result.cache = cache
		result.raw = traced.Tree
		result.trace = traced.Trace
	} else {
		result.Status += "; parser dependency trace is incomplete or cache storage is unavailable"
	}
	return result, nil
}
