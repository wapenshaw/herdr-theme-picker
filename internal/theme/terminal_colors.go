package theme

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"herdr-theme-picker/internal/terminal"
)

// SyncTerminalColors emits OSC color sequences to the outer terminal and updates Ghostty theme file if present.
func SyncTerminalColors(pal *Palette, palettePath string) error {
	if err := validatePalette(pal); err != nil {
		return err
	}
	var syncErrors []error
	// Ghostty fragment persistence if configured
	if home, err := os.UserHomeDir(); err == nil {
		ghosttyDirs := []string{
			filepath.Join(home, ".config", "ghostty", "herdr-theme"),
			filepath.Join(home, "AppData", "Roaming", "ghostty", "herdr-theme"),
		}
		for _, ghosttyTheme := range ghosttyDirs {
			if _, err := os.Stat(ghosttyTheme); err == nil {
				data, err := os.ReadFile(palettePath)
				if err == nil {
					err = atomicWriteFile(ghosttyTheme, data, 0o644)
				}
				if err != nil {
					syncErrors = append(syncErrors, fmt.Errorf("Ghostty theme: %w", err))
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				syncErrors = append(syncErrors, err)
			}
		}
	}

	payload := PaletteOSCPayload(pal)
	if payload == "" {
		return errors.Join(syncErrors...)
	}

	// 1. Emit to current process stdout
	if _, err := fmt.Print(payload); err != nil {
		syncErrors = append(syncErrors, err)
	}

	// 2. Emit to outer host terminal emulator (e.g. Windows Terminal, Ghostty, WezTerm)
	if err := terminal.Emit(payload); err != nil {
		if errors.Is(err, terminal.ErrUnavailable) {
			fmt.Fprintln(os.Stderr, "Host terminal sync skipped: no owning client identified. Set HERDR_THEME_CLIENT_PID to the intended Herdr client PID.")
		} else {
			syncErrors = append(syncErrors, err)
		}
	}
	return errors.Join(syncErrors...)
}

// PaletteOSCPayload builds the OSC 4/10/11 color sequence string for a palette.
func PaletteOSCPayload(pal *Palette) string {
	if pal == nil {
		return ""
	}
	var sb strings.Builder
	if pal.Foreground != "" {
		sb.WriteString(fmt.Sprintf("\033]10;%s\007", pal.Foreground))
	}
	if pal.Background != "" {
		sb.WriteString(fmt.Sprintf("\033]11;%s\007", pal.Background))
	}
	for idx := 0; idx < 16; idx++ {
		col := pal.PaletteColors[idx]
		if col != "" {
			sb.WriteString(fmt.Sprintf("\033]4;%d;%s\007", idx, col))
		}
	}
	return sb.String()
}

// SyncAppliedTheme reads the applied theme slug from AppliedFile() and syncs outer terminal colors.
func SyncAppliedTheme() error {
	appliedPath := AppliedFile()
	data, err := os.ReadFile(appliedPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read applied theme: %w", err)
	}
	slug := strings.TrimSpace(string(data))
	if slug == "" {
		return nil
	}
	palettePath, err := ResolvePalette(slug)
	if err != nil {
		return fmt.Errorf("resolve theme %q: %w", slug, err)
	}
	pal, err := ParsePaletteFile(palettePath)
	if err != nil {
		return fmt.Errorf("parse theme %q: %w", slug, err)
	}
	return SyncTerminalColors(pal, palettePath)
}
