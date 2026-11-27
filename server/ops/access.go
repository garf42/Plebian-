package ops

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CheckAccess opens every file under read for reading and makes a file in every
// directory of write, as the account running it. Install runs it as the server's account
// so a file the root-run installer laid down unusably fails the install, not the server.
func CheckAccess(read, write []string) error {
	var bad []string
	for _, root := range read {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				bad = append(bad, fmt.Sprintf("%s: %v", p, err))
				return nil
			}
			if d.Type().IsRegular() {
				f, err := os.Open(p)
				if err != nil {
					bad = append(bad, fmt.Sprintf("read %s: %v", p, err))
					return nil
				}
				f.Close()
			}
			return nil
		})
	}
	for _, dir := range write {
		f, err := os.CreateTemp(dir, ".access-*")
		if err != nil {
			bad = append(bad, fmt.Sprintf("write %s: %v", dir, err))
			continue
		}
		f.Close()
		os.Remove(f.Name())
	}
	if len(bad) > 0 {
		return fmt.Errorf("the server's account cannot use what install laid down:\n%s", strings.Join(bad, "\n"))
	}
	return nil
}
