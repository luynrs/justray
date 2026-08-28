//go:build darwin

package owner

import (
	"os"
	"strconv"
)

func File(path string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	uid, uidErr := strconv.Atoi(os.Getenv("JUSTRAY_UID"))
	gid, gidErr := strconv.Atoi(os.Getenv("JUSTRAY_GID"))
	if uidErr != nil || gidErr != nil || uid == 0 {
		return nil
	}
	return os.Chown(path, uid, gid)
}
