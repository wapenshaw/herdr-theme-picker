package theme

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"herdr-theme-picker/internal/instance"
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
	if !isStoredUserSlug(slug) {
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
	slugs, err := readUserIndex()
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

// Invalid legacy entries must not disable the whole picker. Keep their bytes
// on disk for recovery on the original OS, but never resolve them as paths.
func readUserIndex() ([]string, error) {
	data, err := os.ReadFile(UserIndexFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var slugs []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		slug := strings.TrimSpace(line)
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		if !isStoredUserSlug(slug) {
			fmt.Fprintf(os.Stderr, "Skipping unsupported theme name %q in %s; its file and index entry are preserved. Rename it on the original system to import it here.\n", slug, UserIndexFile())
			continue
		}
		slugs = append(slugs, slug)
	}
	return slugs, nil
}

// Preserve unrecognized entries rather than silently deleting legacy data when
// another theme is saved or removed.
func updateUserIndex(slug string, remove bool) error {
	lock, err := instance.Acquire(UserIndexFile() + ".lock")
	if err != nil {
		return fmt.Errorf("lock theme index: %w", err)
	}
	defer lock.Close()
	data, err := os.ReadFile(UserIndexFile())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var lines []string
	found := false
	for _, line := range strings.Split(strings.TrimRight(string(data), "\r\n"), "\n") {
		if strings.TrimSpace(line) == slug {
			found = true
			if remove {
				continue
			}
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	if !remove && !found {
		lines = append(lines, slug)
	}
	return atomicWriteFile(UserIndexFile(), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
