package theme

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// WriteCustomBlock replaces or appends the [theme.custom] section in Herdr's config.toml.
func WriteCustomBlock(cfgPath string, tokens map[string]string) error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}

	updated, err := replaceCustomTokens(data, tokens)
	if err != nil {
		return err
	}
	// Keep one backup of the original config each day. Never overwrite it.
	bak := cfgPath + ".bak-" + time.Now().Format("20060102")
	f, err := os.OpenFile(bak, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		_, writeErr := f.Write(data)
		if writeErr == nil {
			writeErr = f.Sync()
		}
		closeErr := f.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			cleanupErr := os.Remove(bak)
			return errors.Join(fmt.Errorf("config backup failed: %w", err), cleanupErr)
		}
	} else if !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("config backup failed: %w", err)
	} else {
		info, statErr := os.Lstat(bak)
		if statErr != nil {
			return fmt.Errorf("check config backup: %w", statErr)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("config backup is not a regular file: %s", bak)
		}
	}
	return atomicWriteFile(cfgPath, updated, 0o600)
}

// Use parser expression ranges rather than matching lines, which can be part of
// multiline strings or arrays. All unrelated bytes (including comments) survive.
func replaceCustomTokens(data []byte, tokens map[string]string) ([]byte, error) {
	var document map[string]any
	if err := toml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("invalid existing TOML: %w", err)
	}
	for key, value := range tokens {
		if !isToken(key) {
			return nil, fmt.Errorf("unknown theme token %q", key)
		}
		if _, err := normalizeColor(value); err != nil {
			return nil, err
		}
	}
	var parser unstable.Parser
	parser.Reset(data)
	var table []string
	var out bytes.Buffer
	last := 0
	inlineTheme := false
	for parser.NextExpression() {
		node := parser.Expression()
		it := node.Key()
		var key []string
		start := -1
		for it.Next() {
			if start < 0 {
				start = int(it.Node().Raw.Offset)
			}
			key = append(key, string(it.Node().Data))
		}
		path := key
		end := start
		if node.Kind == unstable.Table || node.Kind == unstable.ArrayTable {
			table = key
		} else {
			path = append(append([]string{}, table...), key...)
			end = int(node.Raw.Offset + node.Raw.Length)
			if len(path) == 1 && path[0] == "theme" {
				if _, ok := document["theme"].(map[string]any); !ok {
					return nil, fmt.Errorf("theme must be a TOML table")
				}
				inlineTheme = true
			}
		}
		if !(len(path) == 1 && path[0] == "theme" && inlineTheme) && (len(path) < 2 || path[0] != "theme" || path[1] != "custom") {
			continue
		}
		start = bytes.LastIndexByte(data[:start], '\n') + 1
		if n := bytes.IndexByte(data[end:], '\n'); n >= 0 {
			end += n + 1
		} else {
			end = len(data)
		}
		out.Write(data[last:start])
		last = end
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	out.Write(data[last:])
	nl := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		nl = "\r\n"
	}
	if out.Len() > 0 && !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
		out.WriteString(nl)
	}
	if inlineTheme {
		custom := make(map[string]string)
		for key, value := range tokens {
			custom[key], _ = normalizeColor(value)
		}
		theme := document["theme"].(map[string]any)
		theme["custom"] = custom
		encoded, err := toml.Marshal(map[string]any{"theme": theme})
		if err != nil {
			return nil, err
		}
		out.WriteString(nl)
		out.WriteString(strings.ReplaceAll(string(encoded), "\n", nl))
	} else {
		out.WriteString(nl + "[theme.custom]" + nl)
		for _, k := range TokenOrder {
			if v, ok := tokens[k]; ok && v != "" {
				color, _ := normalizeColor(v)
				fmt.Fprintf(&out, "%s = %q%s", k, color, nl)
			}
		}
	}
	if err := toml.Unmarshal(out.Bytes(), &document); err != nil {
		return nil, fmt.Errorf("invalid generated TOML: %w", err)
	}
	return out.Bytes(), nil
}
