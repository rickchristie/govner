package profiles

import (
	"errors"
	"io/fs"
	"os"
)

func checkManagedAttributes(string, fs.FileInfo) error {
	return errors.New("managed profiles require Linux")
}
func checkManagedMounts(string) error       { return errors.New("managed profiles require Linux") }
func checkManagedSpace(string, int64) error { return errors.New("managed profiles require Linux") }
func publishBackup(*os.Root, string, string) error {
	return errors.New("managed profiles require Linux")
}
