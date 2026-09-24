//go:build linux

package resources

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func hostAvailableMemory() (uint64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 || fields[0] != "MemAvailable:" {
			continue
		}
		kib, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || kib > ^uint64(0)/1024 {
			return 0, fmt.Errorf("invalid MemAvailable value")
		}
		return kib * 1024, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("MemAvailable is absent from /proc/meminfo")
}
