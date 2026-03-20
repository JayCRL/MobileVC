//go:build !windows

package runner

func windowsShortPath(path string) (string, error) {
	return path, nil
}
