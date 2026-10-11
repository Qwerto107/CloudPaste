//go:build !windows

package server

import (
	"io/fs"
	"os"
)

// 非 Windows 平台保持文件系统自身的名称匹配规则，不改变 Linux 的大小写语义。
func localCheckOpenedName(_ *os.File, _ string) error          { return nil }
func localCheckName(_ *os.Root, _ string, _ fs.FileInfo) error { return nil }
