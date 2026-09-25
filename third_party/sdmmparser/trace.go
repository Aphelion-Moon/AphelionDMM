package sdmmparser

/*
#include <stdlib.h>
#include <string.h>
#include "lib/sdmmparser.h"
*/
import "C"

import (
	"bytes"
	"errors"
	"unsafe"

	"sdmm/internal/aphelion/diagnostics/uistage"
	"sdmm/internal/aphelion/resources"
)

// APHELION EDIT ADDITION START - PARSER INPUT TRACE
var ErrTraceTransferLimit = errors.New("parser trace output exceeds 256 MiB transfer limit")

// The caller owns the admitted bytes until it finishes decoding or persisting.
func ParseEnvironmentTraced(path string) ([]byte, *resources.Reservation, error) {
	nativePath := C.CString(path)
	defer C.free(unsafe.Pointer(nativePath))
	stage := uistage.Begin("aphelion.parser.native_parse_export_serialize")
	output := C.SdmmParseEnvironmentWithTrace(nativePath)
	stage.End()
	defer C.SdmmFreeStr(output)
	size := uint64(C.strlen(output))
	if size > 256<<20 {
		return nil, nil, ErrTraceTransferLimit
	}
	lease, err := resources.DefaultBudget().Reserve(size*6 + 1<<20)
	if err != nil {
		return nil, nil, err
	}
	stage = uistage.Begin("aphelion.parser.native_transfer")
	data := C.GoBytes(unsafe.Pointer(output), C.int(size))
	stage.End()
	if bytes.HasPrefix(data, []byte("parser error")) || bytes.HasPrefix(data, []byte("error")) {
		lease.Release()
		return nil, nil, &parserError{msg: string(data)}
	}
	return data, lease, nil
}

// APHELION EDIT ADDITION END
