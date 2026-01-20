package webassets

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
)

//go:embed vtuber/*
var embeddedDist embed.FS

// Subdir returns an fs.FS rooted at the requested web subdirectory.
// dir must be a relative path such as "vtuber" or "vtuber/assets".
func Subdir(dir string) (fs.FS, error) {
	cleanDir := path.Clean(dir)
	if cleanDir == "." || cleanDir == "" {
		return embeddedDist, nil
	}

	sub, err := fs.Sub(embeddedDist, cleanDir)
	if err != nil {
		return nil, fmt.Errorf("open embedded dist subdir %q: %w", cleanDir, err)
	}
	return sub, nil
}
