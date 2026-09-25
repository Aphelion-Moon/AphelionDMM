package sdmmparser

/*
#cgo CFLAGS: -I./lib
#cgo LDFLAGS: -L./src/target/release -lsdmmparser
#cgo windows LDFLAGS: -lbcrypt -lntdll
#cgo linux LDFLAGS: -ldl -lm
#include <stdlib.h>
#include "lib/sdmmparser.h"
*/
import "C"
import (
	// APHELION EDIT ADDITION START - UI STAGE TRACE
	"sdmm/internal/aphelion/diagnostics/uistage"
	// APHELION EDIT ADDITION END
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unsafe"
)

type ObjectTreeType struct {
	Location Location
	Path     string
	Vars     []ObjectTreeVar
	Children []ObjectTreeType
}

type Location struct {
	File   string
	Line   uint32
	Column uint16
}

type ObjectTreeVar struct {
	Name     string
	Value    string
	Decl     bool
	IsTmp    bool `json:"is_tmp"`
	IsConst  bool `json:"is_const"`
	IsStatic bool `json:"is_static"`
}

type parserError struct {
	msg string
}

func (e *parserError) Error() string {
	return e.msg
}

func IsParserError(err error) bool {
	var e *parserError
	ok := errors.As(err, &e)
	return ok
}

func ParseEnvironment(environmentPath string) (*ObjectTreeType, error) {
	nativePath := C.CString(environmentPath)
	defer C.free(unsafe.Pointer(nativePath))

	// APHELION EDIT ADDITION START - UI STAGE TRACE
	// Native parsing, constant resolution, tree export and serialization are one
	// FFI call here; the label must not imply separate measured native phases.
	native := uistage.Begin("aphelion.parser.native_parse_export_serialize")
	// APHELION EDIT ADDITION END
	nativeStr := C.SdmmParseEnvironment(nativePath)
	// APHELION EDIT ADDITION START - UI STAGE TRACE
	native.End()
	transfer := uistage.Begin("aphelion.parser.native_transfer_decode")
	defer transfer.End()
	// APHELION EDIT ADDITION END
	defer C.SdmmFreeStr(nativeStr)

	str := C.GoString(nativeStr)
	if strings.HasPrefix(str, "parser error") {
		return nil, &parserError{msg: str}
	}
	if strings.HasPrefix(str, "error") {
		// APHELION EDIT CHANGE - TOOLCHAIN_BASELINE - ORIGINAL: return nil, fmt.Errorf(str)
		return nil, errors.New(str)
	}

	var data ObjectTreeType
	if err := json.Unmarshal([]byte(str), &data); err != nil {
		return nil, fmt.Errorf("unable to deserialize environment: %w", err)
	}

	return &data, nil
}

type IconMetadata struct {
	Width, Height int
	States        []*IconState
}

type IconState struct {
	Name         string
	Dirs, Frames int
}

func ParseIconMetadata(iconPath string) (*IconMetadata, error) {
	nativePath := C.CString(iconPath)
	defer C.free(unsafe.Pointer(nativePath))

	nativeStr := C.SdmmParseIconMetadata(nativePath)
	defer C.SdmmFreeStr(nativeStr)

	str := C.GoString(nativeStr)
	if strings.HasPrefix(str, "error") {
		// APHELION EDIT CHANGE - TOOLCHAIN_BASELINE - ORIGINAL: return nil, fmt.Errorf(str)
		return nil, errors.New(str)
	}

	var data IconMetadata
	if err := json.Unmarshal([]byte(str), &data); err != nil {
		return nil, fmt.Errorf("unable to deserialize icon metadata: %w", err)
	}

	return &data, nil
}
