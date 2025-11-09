package cleanup

import (
	"os"
)

func DeleteDirectory(path string) error {
	return os.RemoveAll(path)
}

func DeleteDirectories(dirs []DirectoryInfo) error {
	for _, dir := range dirs {
		if err := DeleteDirectory(dir.Path); err != nil {
			return err
		}
	}
	return nil
}

func GetTotalSize(dirs []DirectoryInfo) int64 {
	var total int64
	for _, dir := range dirs {
		total += dir.Size
	}
	return total
}

