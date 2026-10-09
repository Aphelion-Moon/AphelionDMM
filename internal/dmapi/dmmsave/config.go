package dmmsave

// APHELION EDIT ADDITION START - KEY_LENGTH_WARNING
import (
	"errors"

	"sdmm/internal/aphelion/mapsave"
)

// APHELION EDIT ADDITION END

type Format uint

const (
	FormatInitial Format = iota
	FormatTGM
	FormatDM
)

type Config struct {
	Format Format

	SanitizeVariables bool

	// APHELION EDIT ADDITION START - KEY_LENGTH_WARNING
	// ConfirmKeyLengthChange, when set, is called on the save goroutine after keys
	// are assigned and before anything is written, but only if the file's key length
	// grows (every tile key changes). Returning false aborts with
	// ErrKeyLengthChangeDeclined and leaves the destination untouched.
	ConfirmKeyLengthChange func(KeyLengthChange) bool
	// APHELION EDIT ADDITION END
}

// APHELION EDIT ADDITION START - KEY_LENGTH_WARNING
// KeyLengthChange describes a save that must re-key the whole file.
type KeyLengthChange struct {
	Path string
	Plan mapsave.KeyLengthPlan
}

// ErrKeyLengthChangeDeclined reports that the caller declined a key length increase.
var ErrKeyLengthChangeDeclined = errors.New("save canceled: key length change declined")

// APHELION EDIT ADDITION END
