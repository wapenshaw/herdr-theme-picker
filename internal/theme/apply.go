package theme

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ApplyTheme resolves a palette for the given slug, writes the [theme.custom] block
// into Herdr's config.toml, updates the state file, and tells Herdr to reload.
func ApplyTheme(slug string) error {
	cfgPath := ConfigPath()
	if _, err := os.Stat(cfgPath); err != nil {
		return fmt.Errorf("config not found: %s", cfgPath)
	}

	palettePath, err := ResolvePalette(slug)
	if err != nil {
		return fmt.Errorf("cannot resolve theme '%s': %w", slug, err)
	}

	pal, err := ParsePaletteFile(palettePath)
	if err != nil {
		return fmt.Errorf("invalid palette for '%s': %w", slug, err)
	}

	tokens, err := PaletteToTokens(pal)
	if err != nil {
		return fmt.Errorf("failed to map tokens: %w", err)
	}

	// Write custom block
	if err := WriteCustomBlock(cfgPath, tokens); err != nil {
		return fmt.Errorf("failed to write custom theme block: %w", err)
	}

	// Sync terminal colors
	syncErr := SyncTerminalColors(pal, palettePath)

	// Reload Herdr
	if err := ReloadHerdr(); err != nil {
		return errors.Join(fmt.Errorf("theme %q written, but reload failed: %w; try: herdr server reload-config", slug, err), syncErr)
	}
	// Mark the theme applied only after the running server accepts the reload.
	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		return errors.Join(fmt.Errorf("theme applied, but state directory failed: %w", err), syncErr)
	}
	if err := atomicWriteFile(AppliedFile(), []byte(slug+"\n"), 0o644); err != nil {
		return errors.Join(fmt.Errorf("theme applied, but applied marker failed: %w", err), syncErr)
	}
	if syncErr != nil {
		return fmt.Errorf("theme %q applied, but terminal sync failed: %w", slug, syncErr)
	}
	fmt.Printf("Applied theme %q.\n", slug)
	return nil
}

// ReloadHerdr executes 'herdr server reload-config'.
func ReloadHerdr() error {
	herdrBin := os.Getenv("HERDR_BIN_PATH")
	if herdrBin == "" {
		herdrBin = "herdr"
	}
	cmd := exec.Command(herdrBin, "server", "reload-config")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
