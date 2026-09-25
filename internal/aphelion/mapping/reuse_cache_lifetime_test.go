package mapping

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmenv"
)

func TestReuseCacheConcurrentCloseAndBorrowRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.dmm")
	if err := os.WriteFile(path, []byte("\"a\" = (/obj/example)\n(1,1,1) = {\"\na\n\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	baseline := resources.DefaultBudget().Used()
	source, err := LoadSource(context.Background(), path, &dmenv.Dme{RootDir: filepath.Dir(path), Objects: map[string]*dmenv.Object{}})
	if err != nil {
		t.Fatal(err)
	}
	cache := NewReuseCache()
	defer cache.Close()
	_, first := cache.publishSource(source)
	if first == nil {
		source.Close()
		t.Fatal("source was not retained")
	}
	_, second := cache.acquireSource(path, source.Identity.EnvironmentHash)
	if second == nil {
		first.release()
		t.Fatal("second borrow failed")
	}
	start := make(chan struct{})
	var done sync.WaitGroup
	for _, release := range []func(){cache.Close, first.release, second.release} {
		done.Add(1)
		go func() {
			defer done.Done()
			<-start
			release()
		}()
	}
	close(start)
	done.Wait()
	if got := resources.DefaultBudget().Used(); got != baseline {
		t.Fatalf("concurrent teardown retained source reservation: got %d, baseline %d", got, baseline)
	}
}
