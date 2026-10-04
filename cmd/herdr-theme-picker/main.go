package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"herdr-theme-picker/internal/terminal"
	"herdr-theme-picker/internal/theme"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		runPicker()
		return
	}

	switch args[0] {
	case "help", "--help", "-h":
		fmt.Println("Usage: herdr-theme-picker [picker | apply <slug> | sync [--client <pid>] | preview <slug> | add [--clipboard] | edit <slug> | delete <slug>]")
		fmt.Println("Themes are written to this machine's Herdr config. Clients attached with herdr --remote use their own machine's config: run the picker there.")
		return
	case "client-setup":
		clientSetup()
		return
	case "startup":
		// Old manifests registered a startup daemon. Terminal colors are now
		// synced only when a theme is applied, so this is a no-op.
		return

	case "sync":
		if len(args) == 3 && args[1] == "--client" {
			os.Setenv("HERDR_THEME_CLIENT_PID", args[2])
		} else if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "usage: herdr-theme-picker sync [--client <pid>]")
			os.Exit(1)
		}
		if err := theme.SyncAppliedTheme(); err != nil {
			if theme.IsOuterTerminalUnavailable(err) {
				fmt.Fprintln(os.Stderr, "No Herdr client terminal found on this machine.")
			} else {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
			os.Exit(1)
		}

	case "terminal-sync":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: herdr-theme-picker terminal-sync <client-pid>")
			os.Exit(1)
		}
		if err := terminal.RunHelper(args[1]); err != nil {
			if theme.IsOuterTerminalUnavailable(err) {
				os.Exit(3)
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "preview-file":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: herdr-theme-picker preview-file <palette-path>")
			os.Exit(1)
		}
		fmt.Print(theme.RenderSwatchFile(args[1], "editing"))
	case "picker":
		runPicker()

	case "open":
		herdrBin := os.Getenv("HERDR_BIN_PATH")
		if herdrBin == "" {
			herdrBin = "herdr"
		}
		cmd := exec.Command(herdrBin, "plugin", "pane", "open",
			"--plugin", "herdr-theme-picker",
			"--entrypoint", "picker",
			"--placement", "popup",
			"--focus",
		)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open theme picker pane: %v\n", err)
			os.Exit(1)
		}

	case "preview", "--preview":
		if len(args) < 2 {
			fmt.Println("usage: herdr-theme-picker preview <slug>")
			return
		}
		row := strings.Join(args[1:], " ")
		if strings.HasPrefix(row, "+ Add from clipboard") {
			fmt.Println("  Reads a ghostty-format palette from your clipboard,")
			fmt.Println("  asks a name, saves and applies — no editor.")
			return
		}
		if strings.HasPrefix(row, "+ Add new theme") {
			fmt.Println("  Opens an editor to paste a")
			fmt.Println("  ghostty-format theme; name it, saved and applied.")
			return
		}

		bare := strings.TrimPrefix(row, "✓ ")
		bare = strings.TrimPrefix(bare, "★ ")
		bare = strings.TrimSpace(bare)
		fmt.Print(theme.RenderSwatch(bare))

	case "apply":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: herdr-theme-picker apply <slug>")
			os.Exit(1)
		}
		if err := theme.ApplyTheme(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "add":
		var slug string
		var err error
		if len(args) > 1 && args[1] == "--clipboard" {
			slug, err = theme.AddThemeClipboard()
		} else {
			slug, err = theme.AddThemeEditor()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to add theme: %v\n", err)
			os.Exit(1)
		}
		if slug != "" {
			if err := theme.ApplyTheme(slug); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to apply added theme: %v\n", err)
				os.Exit(1)
			}
		}

	case "edit":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: herdr-theme-picker edit <slug>")
			os.Exit(1)
		}
		if err := theme.EditTheme(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "delete":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: herdr-theme-picker delete <slug>")
			os.Exit(1)
		}
		if err := theme.DeleteTheme(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	default:
		// Fallback: if argument is a slug, treat as apply
		if theme.IsValidSlug(args[0]) {
			if err := theme.ApplyTheme(args[0]); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		os.Exit(1)
	}
}

// runPicker opens the picker unless it is running inside Herdr while only
// herdr --remote clients are attached. Then whoever opened it is on another
// machine whose theme comes from its own config, so changing this machine's
// config would not affect them; explain where to run it instead.
func runPicker() {
	if os.Getenv("HERDR_ENV") != "" {
		if processes, err := terminal.Processes(); err == nil && terminal.OnlyRemoteClients(processes) {
			clientSetup()
			return
		}
	}
	if err := theme.RunPicker(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func clientSetup() {
	fmt.Println("Themes belong to your client machine.\n\nOpen a local terminal outside Herdr and run:\n\n  herdr-theme-picker picker\n\nThen use Reload config in the Herdr client you want to update.\nInstall the picker locally if you are connected to a remote server.\nThis server's theme and terminal colors are left unchanged.\n\nPress Enter to close.")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}
