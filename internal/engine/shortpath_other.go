//go:build !windows

package engine

func windowsShortPath(path string) (string, error) {
	return path, nil
}
