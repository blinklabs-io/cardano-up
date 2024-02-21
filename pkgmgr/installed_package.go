package pkgmgr

import (
	"time"
)

type InstalledPackage struct {
	Package       Package
	InstalledTime time.Time
	Context       string
}

func NewInstalledPackage(pkg Package, context string) InstalledPackage {
	return InstalledPackage{
		Package:       pkg,
		InstalledTime: time.Now(),
		Context:       context,
	}
}
