package cotw

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// cotwAppID is theHunter: Call of the Wild on Steam.
const cotwAppID = "518790"

// installFolderName is the folder COTW installs into.
const installFolderName = "theHunterCotW"

// steamRootCandidates lists the usual Steam locations.
func steamRootCandidates() []string {
	var out []string
	if p := os.Getenv("ProgramFiles(x86)"); p != "" {
		out = append(out, filepath.Join(p, "Steam"))
	}
	if p := os.Getenv("ProgramFiles"); p != "" {
		out = append(out, filepath.Join(p, "Steam"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, "Steam"))
	}
	for _, drive := range []string{"C:", "D:", "E:", "F:", "G:"} {
		out = append(out, filepath.Join(drive+string(filepath.Separator), "SteamLibrary"),
			filepath.Join(drive+string(filepath.Separator), "Steam"))
	}
	return dedupeStrings(out)
}

var vdfPathPattern = regexp.MustCompile(`"path"\s+"([^"]+)"`)

// steamLibraryRoots returns every Steam library folder it can find.
func steamLibraryRoots() []string {
	roots := steamRootCandidates()
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = filepath.Clean(p)
		if p == "" || seen[strings.ToLower(p)] {
			return
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			seen[strings.ToLower(p)] = true
			out = append(out, p)
		}
	}
	for _, root := range roots {
		add(root)
		data, err := os.ReadFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
		if err != nil {
			continue
		}
		for _, m := range vdfPathPattern.FindAllStringSubmatch(string(data), -1) {
			add(strings.ReplaceAll(m[1], `\\`, `\`))
		}
	}
	return out
}

// DefaultInstallDir finds the COTW install folder through the Steam libraries.
func DefaultInstallDir() (string, error) {
	var tried []string
	for _, root := range steamLibraryRoots() {
		candidate := filepath.Join(root, "steamapps", "common", installFolderName)
		tried = append(tried, candidate)
		if info, err := os.Stat(filepath.Join(candidate, "archives_win64")); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("cotw: could not find %s in any Steam library (looked in %s)",
		installFolderName, strings.Join(tried, ", "))
}

// detectGameVersion reports a value that changes when the game is patched, plus
// where it came from.
func detectGameVersion(installDir string) (string, string) {
	if v, ok := versionFromStamp(installDir); ok {
		return v, "version stamp"
	}
	if v, ok := versionFromManifest(installDir); ok {
		return v, "steam app manifest"
	}
	if v, ok := versionFromFiles(installDir); ok {
		return v, "archive signature"
	}
	return "unknown", "none"
}

// versionFromStamp reads the small json file the game writes next to its exe.
func versionFromStamp(installDir string) (string, bool) {
	matches, err := filepath.Glob(filepath.Join(installDir, "versions.*.txt"))
	if err != nil || len(matches) == 0 {
		return "", false
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return "", false
	}
	var stamp struct {
		Code     int64           `json:"code_final"`
		Archives json.RawMessage `json:"archives"`
	}
	if err := json.Unmarshal(data, &stamp); err != nil || stamp.Code == 0 {
		return "", false
	}
	archives := strings.Trim(string(stamp.Archives), `"`)
	return fmt.Sprintf("code=%d archives=%s", stamp.Code, archives), true
}

// versionFromManifest reads the Steam app manifest's build id.
func versionFromManifest(installDir string) (string, bool) {
	dir, err := filepath.Abs(installDir)
	if err != nil {
		return "", false
	}
	for i := 0; i < 6; i++ {
		dir = filepath.Dir(dir)
		manifest := filepath.Join(dir, "steamapps", "appmanifest_"+cotwAppID+".acf")
		data, err := os.ReadFile(manifest)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, `"buildid"`) {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				id := strings.Trim(fields[len(fields)-1], `"`)
				if id != "" && id != cotwAppID {
					return "buildid=" + id, true
				}
			}
		}
	}
	return "", false
}

// versionFromFiles falls back to a signature over the archive files, which
// changes whenever Steam replaces any of them.
func versionFromFiles(installDir string) (string, bool) {
	dir := filepath.Join(installDir, "archives_win64")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "%s|%d|%d\n", name, info.Size(), info.ModTime().Unix())
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if len(sum) < 16 {
		return "", false
	}
	return "files=" + sum[:16], true
}

// SaveRoots are the folders COTW writes its saves to, in probe order.
func SaveRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, "Documents", "Avalanche Studios", "COTW", "Saves"),
		filepath.Join(home, "Documents", "Avalanche Studios", "Epic Games Store", "COTW", "Saves"),
		filepath.Join(home, "OneDrive", "Documents", "Avalanche Studios", "COTW", "Saves"),
		filepath.Join(home, "OneDrive", "Documents", "Avalanche Studios", "Epic Games Store", "COTW", "Saves"),
	}
}

var accountDirPattern = regexp.MustCompile(`^\d+$`)

// FindSaveDirs returns every per-account save folder under the known roots.
func FindSaveDirs() []string {
	var dirs []string
	for _, root := range SaveRoots() {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && accountDirPattern.MatchString(e.Name()) {
				dirs = append(dirs, filepath.Join(root, e.Name()))
			}
		}
	}
	return dirs
}

// DefaultSaveDir returns the first save folder found.
func DefaultSaveDir() (string, error) {
	dirs := FindSaveDirs()
	if len(dirs) == 0 {
		return "", fmt.Errorf("cotw: no save folder found (looked in %s)", strings.Join(SaveRoots(), ", "))
	}
	return dirs[0], nil
}

// parseSteamID pulls the account id out of a save folder path.
func parseSteamID(dir string) string {
	base := filepath.Base(dir)
	if accountDirPattern.MatchString(base) {
		return base
	}
	return ""
}

// atoiOr parses s, returning fallback when it is not a number.
func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return fallback
}
