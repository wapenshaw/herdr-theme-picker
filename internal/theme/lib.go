package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

var validSlugRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// IsValidSlug reports whether slug consists only of lowercase letters, digits, and hyphens.
func IsValidSlug(slug string) bool {
	if len(slug) > 120 || !validSlugRe.MatchString(slug) {
		return false
	}
	// These names cannot be created as normal files on Windows.
	switch slug {
	case "con", "prn", "aux", "nul", "com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9", "lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		return false
	}
	return true
}

// Slugify converts an arbitrary name into a slug: lowercase, letters/numbers/hyphens only.
func Slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")

	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	s = b.String()
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	return s
}

// DarkenHex darkens a hex color (#rrggbb or rrggbb) by the specified percent [0..100].
func DarkenHex(hexStr string, percent int) string {
	hex := strings.TrimPrefix(hexStr, "#")
	if len(hex) != 6 {
		return hexStr
	}
	r64, _ := strconv.ParseInt(hex[0:2], 16, 64)
	g64, _ := strconv.ParseInt(hex[2:4], 16, 64)
	b64, _ := strconv.ParseInt(hex[4:6], 16, 64)

	factor := int64(100 - percent)
	if factor < 0 {
		factor = 0
	}
	r := (r64 * factor) / 100
	g := (g64 * factor) / 100
	b := (b64 * factor) / 100

	if r < 0 {
		r = 0
	} else if r > 255 {
		r = 255
	}
	if g < 0 {
		g = 0
	} else if g > 255 {
		g = 255
	}
	if b < 0 {
		b = 0
	} else if b > 255 {
		b = 255
	}

	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// ParseRGB parses a #rrggbb hex string into red, green, blue integer components.
func ParseRGB(hexStr string) (int, int, int) {
	hex := strings.TrimPrefix(hexStr, "#")
	if len(hex) != 6 {
		return 0, 0, 0
	}
	r, _ := strconv.ParseInt(hex[0:2], 16, 32)
	g, _ := strconv.ParseInt(hex[2:4], 16, 32)
	b, _ := strconv.ParseInt(hex[4:6], 16, 32)
	return int(r), int(g), int(b)
}

// ConfigPath returns the location of Herdr's config.toml across platforms.
func ConfigPath() string {
	if custom := os.Getenv("HERDR_CONFIG_PATH"); custom != "" {
		return custom
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "herdr", "config.toml")
		}
	} else if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			macSupport := filepath.Join(home, "Library", "Application Support", "herdr", "config.toml")
			if _, err := os.Stat(macSupport); err == nil {
				return macSupport
			}
		}
	}

	// Default fallback: XDG or ~/.config
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "herdr", "config.toml")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "herdr", "config.toml")
	}
	return "config.toml"
}

// HerdrSocketPath returns the path to Herdr's socket file.
func HerdrSocketPath() string {
	if custom := os.Getenv("HERDR_SOCKET_PATH"); custom != "" {
		return custom
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "herdr", "herdr.sock")
		}
	} else {
		if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
			return filepath.Join(runtimeDir, "herdr", "herdr.sock")
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".config", "herdr", "herdr.sock")
		}
	}
	return ""
}

// HerdrServerPID reads the PID of the running Herdr server from herdr.sock.
func HerdrServerPID() int {
	sockPath := HerdrSocketPath()
	if sockPath != "" {
		data, err := os.ReadFile(sockPath)
		if err == nil {
			parts := strings.Split(strings.TrimSpace(string(data)), ":")
			if len(parts) > 0 {
				if pid, err := strconv.Atoi(parts[0]); err == nil && pid > 0 {
					return pid
				}
			}
		}
	}
	return os.Getppid()
}

// PluginRoot returns the directory containing plugin assets (themes, herdr-plugin.toml).
func PluginRoot() string {
	if root := os.Getenv("HERDR_PLUGIN_ROOT"); root != "" {
		return root
	}
	exe, err := os.Executable()
	if err == nil {
		exe = strings.TrimPrefix(exe, `\\?\`)
		dir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(dir, "themes")); err == nil {
			return dir
		}
	}
	cwd, err := os.Getwd()
	if err == nil {
		if _, err := os.Stat(filepath.Join(cwd, "themes")); err == nil {
			return cwd
		}
		parent := filepath.Dir(cwd)
		if _, err := os.Stat(filepath.Join(parent, "themes")); err == nil {
			return parent
		}
		grandParent := filepath.Dir(parent)
		if _, err := os.Stat(filepath.Join(grandParent, "themes")); err == nil {
			return grandParent
		}
	}
	return "."
}

// StateDir returns the directory where plugin state (applied theme, user themes) is persisted.
func StateDir() string {
	if state := os.Getenv("HERDR_PLUGIN_STATE_DIR"); state != "" {
		return state
	}
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "herdr-theme-picker")
		}
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "herdr", "plugins", "state", "herdr-theme-picker")
		}
	}
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "herdr-theme-picker")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cache", "herdr-theme-picker")
	}
	return filepath.Join(".", ".state")
}

// CacheDir returns the download cache directory for remote themes.
func CacheDir() string {
	return filepath.Join(StateDir(), "cache")
}

// UserThemesDir returns the directory storing user-added themes.
func UserThemesDir() string {
	return filepath.Join(StateDir(), "themes")
}

// UserIndexFile returns the path to the user themes index list.
func UserIndexFile() string {
	return filepath.Join(StateDir(), "index.txt")
}

// AppliedFile returns the path to the file storing the active theme slug.
func AppliedFile() string {
	return filepath.Join(StateDir(), "applied")
}

// IsUserTheme reports whether slug is an existing user-added theme.
func IsUserTheme(slug string) bool {
	_, err := userThemePath(slug)
	return err == nil
}
