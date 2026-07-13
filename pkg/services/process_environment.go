package services

import (
	"os"
	"runtime"
	"strings"
)

var generatorEnvironmentAllowList = map[string]bool{
	"HOME":                      true,
	"PATH":                      true,
	"TMP":                       true,
	"TEMP":                      true,
	"TMPDIR":                    true,
	"USERPROFILE":               true,
	"LOCALAPPDATA":              true,
	"APPDATA":                   true,
	"MISE_DATA_DIR":             true,
	"MISE_CACHE_DIR":            true,
	"MISE_CONFIG_DIR":           true,
	"MISE_INSTALL_PATH":         true,
	"MISE_TRUSTED_CONFIG_PATHS": true,
}

var windowsGeneratorEnvironmentAllowList = map[string]bool{
	"COMSPEC":           true,
	"PATHEXT":           true,
	"SYSTEMDRIVE":       true,
	"SYSTEMROOT":        true,
	"WINDIR":            true,
	"PROGRAMDATA":       true,
	"PROGRAMFILES":      true,
	"PROGRAMFILES(X86)": true,
}

func generatorProcessEnvironment(extra ...string) []string {
	env := make([]string, 0, len(generatorEnvironmentAllowList)+len(windowsGeneratorEnvironmentAllowList)+len(extra))
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		upperKey := strings.ToUpper(key)
		if generatorEnvironmentAllowList[upperKey] || (runtime.GOOS == "windows" && windowsGeneratorEnvironmentAllowList[upperKey]) {
			env = append(env, entry)
		}
	}
	env = append(env, extra...)
	return env
}
