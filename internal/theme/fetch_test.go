package theme

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadValidationAndCacheRecovery(t *testing.T) {
	valid := fixturePalette(t)
	for _, kind := range []string{"valid", "invalid", "oversize", "not-found", "partial-cache"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/test-remote" {
					t.Errorf("unexpected URL: %s", r.URL.Path)
				}
				switch kind {
				case "invalid":
					fmt.Fprint(w, "palette = 0=#123456\n")
				case "oversize":
					fmt.Fprint(w, strings.Repeat("x", maxPaletteBytes+1))
				case "not-found":
					http.NotFound(w, r)
				default:
					fmt.Fprint(w, valid)
				}
			}))
			defer server.Close()
			cache := filepath.Join(CacheDir(), "test-remote")
			if kind == "partial-cache" {
				writeTestFile(t, cache, "palette = 0=#123456\n")
			}
			path, err := resolvePalette("test-remote", server.URL, server.Client())
			if kind != "valid" && kind != "partial-cache" {
				if err == nil {
					t.Fatal("bad download accepted")
				}
				if _, err := os.Stat(cache); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("bad download cached")
				}
				return
			}
			if err != nil || path != cache {
				t.Fatalf("download failed: %v", err)
			}
			if _, err := ParsePaletteFile(path); err != nil {
				t.Fatal(err)
			}
			if _, err := resolvePalette("test-remote", server.URL, server.Client()); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("valid cache downloaded again: %d calls", calls)
			}
		})
	}
}

func TestBashCacheIsReusedOffline(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(legacyCacheDir(), "old-download")
	writeTestFile(t, path, fixturePalette(t))
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	got, err := resolvePalette("old-download", server.URL, server.Client())
	if err != nil || got != path || calls != 0 {
		t.Fatalf("ignored valid Bash cache: path=%s calls=%d err=%v", got, calls, err)
	}
	writeTestFile(t, path, "invalid")
	if _, err := resolvePalette("old-download", server.URL, server.Client()); err == nil {
		t.Fatal("invalid Bash cache accepted")
	}
	if calls != 1 {
		t.Fatal("invalid legacy cache prevented fetch")
	}
}
