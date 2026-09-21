//go:build !linux

package profiles

import "os"

func renameEntry(*os.Root, string, string) error { return Supported() }
func checkRootMounts(string) error               { return Supported() }
