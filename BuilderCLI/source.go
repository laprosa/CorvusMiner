package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Directories inside Client/ that are build artefacts, not sources.
var skipDirs = map[string]bool{
	"build":  true,
	"backup": true,
	"dist":   true,
	".git":   true,
}

// panelURLRe and configURLRe match the two OBFUSCATE_STRING literals that carry
// the runtime configuration endpoints in Client/src/main.cpp.
var (
	panelURLRe  = regexp.MustCompile(`(std::string\s+panelUrlsStr\s*=\s*OBFUSCATE_STRING\s*\(\s*")[^"]*("\s*\)\s*;)`)
	configURLRe = regexp.MustCompile(`(std::string\s+configGetUrlStr\s*=\s*OBFUSCATE_STRING\s*\(\s*")[^"]*("\s*\)\s*;)`)
)

// resolveClientDir returns the Client source directory and the repository root.
// When explicit is empty the directory is discovered by walking up from the
// working directory and then from the executable location.
func resolveClientDir(explicit string) (clientDir, root string, err error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", "", fmt.Errorf("-source: %w", err)
		}
		if !isClientDir(abs) {
			return "", "", fmt.Errorf("%s is not a Client source directory (expected CMakeLists.txt and src/main.cpp)", abs)
		}
		return abs, filepath.Dir(abs), nil
	}

	var starts []string
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		dir := start
		for depth := 0; depth < 8; depth++ {
			candidate := filepath.Join(dir, "Client")
			if isClientDir(candidate) {
				return candidate, dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", "", errors.New("could not locate Client/CMakeLists.txt; run from the repository root or pass -source")
}

func isClientDir(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "CMakeLists.txt")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "src", "main.cpp")); err != nil {
		return false
	}
	return true
}

// stageClient copies the client source tree into dst so that URL patching and
// the build itself never mutate the repository. This also makes concurrent
// builds safe.
func stageClient(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return fs.SkipDir
		}

		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := out.ReadFrom(in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// patchMainCpp replaces the panel/config URLs in main.cpp. It fails loudly when
// a pattern is missing, because a silently unpatched binary would point at the
// default localhost panel.
func patchMainCpp(path, panelURLs, configURLs string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	updated := data
	if strings.TrimSpace(panelURLs) != "" {
		joined := strings.Join(splitTrimmed(panelURLs), ",")
		updated, err = replaceLiteral(updated, panelURLRe, joined)
		if err != nil {
			return fmt.Errorf("panelUrlsStr: %w", err)
		}
	}
	if strings.TrimSpace(configURLs) != "" {
		joined := strings.Join(splitTrimmed(configURLs), ",")
		updated, err = replaceLiteral(updated, configURLRe, joined)
		if err != nil {
			return fmt.Errorf("configGetUrlStr: %w", err)
		}
	}

	if string(updated) == string(data) {
		return nil
	}
	return os.WriteFile(path, updated, 0o644)
}

func replaceLiteral(src []byte, re *regexp.Regexp, value string) ([]byte, error) {
	matches := re.FindAllSubmatchIndex(src, -1)
	if len(matches) != 1 {
		return nil, fmt.Errorf("expected exactly one match in src/main.cpp, found %d", len(matches))
	}
	m := matches[0]
	// m[3] is the end of the opening literal, m[4] the start of the closing one.
	out := make([]byte, 0, len(src)+len(value))
	out = append(out, src[:m[3]]...)
	out = append(out, value...)
	out = append(out, src[m[4]:]...)
	return out, nil
}
