// Command i18n-validate is the build-time translation validator required by
// AI.md PART 30 "Build-Time Validation". It loads every locales/*.json file in
// the directory given as the first argument and enforces:
//
//   - All language files have identical key sets to en.json.
//   - No empty string values.
//   - All interpolation variables ({var}) match across languages.
//   - No orphaned keys (keys in other languages not in en.json).
//
// It exits non-zero and prints every violation when validation fails, so it
// can gate CI and `make i18n-validate`.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// baseLang is the authoritative language every other locale is compared to.
const baseLang = "en"

// varPattern matches interpolation tokens such as {count} or {app_name}.
var varPattern = regexp.MustCompile(`\{[a-zA-Z0-9_]+\}`)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: i18n-validate <locales-dir>")
		os.Exit(2)
	}
	dir := os.Args[1]

	locales, err := loadLocales(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	base, ok := locales[baseLang]
	if !ok {
		fmt.Fprintf(os.Stderr, "error: missing base locale %s.json in %s\n", baseLang, dir)
		os.Exit(2)
	}

	var problems []string
	for _, lang := range sortedKeys(locales) {
		if lang == baseLang {
			problems = append(problems, checkValues(lang, locales[lang])...)
			continue
		}
		problems = append(problems, compareToBase(lang, base, locales[lang])...)
		problems = append(problems, checkValues(lang, locales[lang])...)
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, p)
		}
		fmt.Fprintf(os.Stderr, "\ni18n-validate: FAILED with %d problem(s)\n", len(problems))
		os.Exit(1)
	}

	fmt.Printf("i18n-validate: OK (%d languages, %d keys each)\n", len(locales), len(base))
}

// loadLocales reads every *.json file in dir and returns a map of language code
// to its flattened key/value pairs.
func loadLocales(dir string) (map[string]map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var tree map[string]interface{}
		if err := json.Unmarshal(data, &tree); err != nil {
			return nil, fmt.Errorf("%s: invalid JSON: %w", e.Name(), err)
		}
		flat := map[string]string{}
		flatten("", tree, flat)
		out[strings.TrimSuffix(e.Name(), ".json")] = flat
	}
	return out, nil
}

// flatten collapses a nested JSON object into dot-separated keys.
func flatten(prefix string, node map[string]interface{}, out map[string]string) {
	for k, v := range node {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]interface{}:
			flatten(key, val, out)
		case string:
			out[key] = val
		case float64:
			out[key] = strconv.FormatFloat(val, 'f', -1, 64)
		case bool:
			out[key] = strconv.FormatBool(val)
		}
	}
}

// compareToBase reports missing keys, orphaned keys, and interpolation-variable
// mismatches for lang relative to the base locale.
func compareToBase(lang string, base, target map[string]string) []string {
	var problems []string
	for key, baseVal := range base {
		targetVal, ok := target[key]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: missing key %q (present in %s)", lang, key, baseLang))
			continue
		}
		if bv, tv := varSet(baseVal), varSet(targetVal); !equalSets(bv, tv) {
			problems = append(problems, fmt.Sprintf("%s: key %q interpolation vars %v differ from %s %v", lang, key, sortedSet(tv), baseLang, sortedSet(bv)))
		}
	}
	for key := range target {
		if _, ok := base[key]; !ok {
			problems = append(problems, fmt.Sprintf("%s: orphaned key %q (not in %s)", lang, key, baseLang))
		}
	}
	return problems
}

// checkValues reports any empty string value, which the spec forbids.
func checkValues(lang string, target map[string]string) []string {
	var problems []string
	for key, val := range target {
		if strings.TrimSpace(val) == "" {
			problems = append(problems, fmt.Sprintf("%s: empty value for key %q", lang, key))
		}
	}
	return problems
}

func varSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, m := range varPattern.FindAllString(s, -1) {
		set[m] = true
	}
	return set
}

func equalSets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
