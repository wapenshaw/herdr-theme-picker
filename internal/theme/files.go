package theme

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// atomicWriteFile keeps readers from observing truncated configs, indexes or palettes.
// Resolve an existing symlink so updating a linked config preserves the link.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			path, err = filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			info, err = os.Stat(path)
			if err != nil {
				return err
			}
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("not a regular file: %s", path)
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".theme-picker-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return replaceFile(f.Name(), path)
}

func readIndex(path string, optional bool) ([]string, error) {
	data, err := os.ReadFile(path)
	if optional && errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var slugs []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		slug := strings.TrimSpace(line)
		if slug == "" {
			continue
		}
		if !IsValidSlug(slug) {
			return nil, fmt.Errorf("invalid slug %q in %s", slug, path)
		}
		if !seen[slug] {
			slugs = append(slugs, slug)
			seen[slug] = true
		}
	}
	return slugs, nil
}

func userThemePath(slug string) (string, error) {
	if !IsValidSlug(slug) {
		return "", fmt.Errorf("invalid slug: %q", slug)
	}
	path := filepath.Join(UserThemesDir(), slug)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	// User theme operations never follow links outside their storage directory.
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("user theme is not a regular file: %s", slug)
	}
	slugs, err := readIndex(UserIndexFile(), false)
	if err != nil {
		return "", err
	}
	for _, item := range slugs {
		if item == slug {
			return path, nil
		}
	}
	return "", fmt.Errorf("%q is not a user-added theme", slug)
}
