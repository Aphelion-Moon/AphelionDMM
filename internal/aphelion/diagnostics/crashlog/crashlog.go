// Package crashlog keeps the stack trace of a crash. The Windows desktop build
// has no console, so the runtime's report of an unrecovered panic or a fatal
// error (such as a concurrent map write) on stderr is otherwise lost, and the
// session log simply stops.
package crashlog

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"
)

// Capture makes the runtime also write its crash report to file, after a
// marker line. It covers panics on every goroutine and unrecoverable runtime
// errors. The runtime duplicates the descriptor, so file may be closed later.
func Capture(file *os.File) error {
	if file == nil {
		return fmt.Errorf("no crash log file")
	}
	if err := debug.SetCrashOutput(file, debug.CrashOptions{}); err != nil {
		return err
	}
	_, err := fmt.Fprintf(file, "%s INF crash reports for this session are appended below if the process crashes\n", time.Now().Format(time.DateTime))
	return err
}
