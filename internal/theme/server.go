package theme

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"herdr-theme-picker/internal/terminal"
)

// HerdrSocketPath honors the session socket supplied by Herdr. Otherwise its
// default socket lives alongside config.toml on each supported platform.
func HerdrSocketPath() string {
	if custom := os.Getenv("HERDR_SOCKET_PATH"); custom != "" {
		return custom
	}
	return filepath.Join(filepath.Dir(ConfigPath()), "herdr.sock")
}

// HerdrServerPID reads Windows' PID metadata file. Unix sockets contain no PID,
// so startup hooks use their Herdr ancestor. A shell is never a server owner.
func HerdrServerPID() int {
	if runtime.GOOS == "windows" {
		if data, err := os.ReadFile(HerdrSocketPath()); err == nil {
			pidText, _, _ := strings.Cut(strings.TrimSpace(string(data)), ":")
			if pid, err := strconv.Atoi(pidText); err == nil && pid > 0 {
				return pid
			}
		}
	}
	processes, err := terminal.Processes()
	if err != nil {
		return 0
	}
	parent := os.Getppid()
	seen := make(map[int]bool)
	for parent > 0 && !seen[parent] {
		seen[parent] = true
		p, ok := processes[parent]
		if !ok {
			break
		}
		if terminal.IsHerdr(p.Name) {
			return p.PID
		}
		parent = p.Parent
	}
	return 0
}
