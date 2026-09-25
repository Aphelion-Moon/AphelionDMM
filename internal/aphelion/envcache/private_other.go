//go:build !windows

package envcache

import (
	"fmt"
	"os"
	"syscall"
)

func privateStorage(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) || info.Mode().Perm()&0022 != 0 || info.Mode()&os.ModeSymlink != 0 || info.IsDir() != directory {
		return fmt.Errorf("cache path is not private user-owned storage")
	}
	return nil
}
