package theme

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const baseURL = "https://terminalcolors.com/downloads/ghostty"

// ResolvePalette gives custom themes precedence over bundled and cached themes.
func ResolvePalette(slug string) (string, error) {
	return resolvePalette(slug, baseURL, &http.Client{Timeout: 15 * time.Second})
}

func resolvePalette(slug, remoteURL string, client *http.Client) (string, error) {
	if !IsValidSlug(slug) {
		return "", fmt.Errorf("invalid slug: %s", slug)
	}

	// 1. User-added theme. This order also repairs existing name collisions.
	userPath := filepath.Join(UserThemesDir(), slug)
	if info, err := os.Lstat(userPath); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("user theme is not a regular file: %s", slug)
		}
		if _, err := ParsePaletteFile(userPath); err != nil {
			return "", fmt.Errorf("invalid user theme: %w", err)
		}
		return userPath, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	// 2. Bundled theme
	bundlePath := filepath.Join(PluginRoot(), "themes", slug)
	if info, err := os.Stat(bundlePath); err == nil && !info.IsDir() {
		return bundlePath, nil
	}

	// 3. Cached theme
	cacheDir := CacheDir()
	cachePath := filepath.Join(cacheDir, slug)
	if info, err := os.Lstat(cachePath); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("cache is not a regular file: %s", slug)
		}
		if _, err := ParsePaletteFile(cachePath); err == nil {
			return cachePath, nil
		}
		// Invalid/partial caches are fetched again instead of poisoning future picks.
	} else if !os.IsNotExist(err) {
		return "", err
	}

	// 4. Remote fetch
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create cache dir: %w", err)
	}

	url := fmt.Sprintf("%s/%s", remoteURL, slug)
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch '%s' (offline or not found): %w", slug, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch '%s' (status %d)", slug, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPaletteBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if _, err := ParsePaletteContent(string(body)); err != nil {
		return "", fmt.Errorf("invalid downloaded palette for %q: %w", slug, err)
	}

	if err := atomicWriteFile(cachePath, body, 0o644); err != nil {
		return "", fmt.Errorf("failed to write cache: %w", err)
	}

	return cachePath, nil
}
