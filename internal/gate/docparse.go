package gate

// Reading the document side of a contract. A document is text, so a pattern or
// a line scan is the right reader here; only the Go side has to be parsed.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var (
	reJSONKey   = regexp.MustCompile(`"([A-Za-z0-9_]+)"\s*:`)
	reTableCode = regexp.MustCompile("(?m)^[|]\\s*\\x60([0-9])\\x60\\s*[|]")
)

func fencedBlocks(doc, lang string) []string {
	var out []string
	lines := strings.Split(doc, "\n")
	inBlock := false
	var body []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if trimmed == "```"+lang {
				inBlock = true
				body = nil
			}
			continue
		}
		if trimmed == "```" {
			inBlock = false
			out = append(out, strings.Join(body, "\n"))
			continue
		}
		body = append(body, line)
	}
	return out
}

// jsonKeysInText returns the `"Name":` keys of a JSON block, which is how the
// human copy spells its field names. A document is text, so a pattern is the
// right reader for it; it is the Go side of this comparison that must be read as
// declarations, which is why the engine's tags come from the parser instead.
func jsonKeysInText(block string) []string {
	var out []string
	for _, m := range reJSONKey.FindAllStringSubmatch(block, -1) {
		out = append(out, m[1])
	}
	return out
}

// sectionAfter returns the text between an anchor line and the next blank line,
// which is how a titled block in the help text is delimited.
func sectionAfter(text, anchor string) (string, bool) {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != anchor {
			continue
		}
		var body []string
		for _, rest := range lines[i+1:] {
			if strings.TrimSpace(rest) == "" {
				break
			}
			body = append(body, rest)
		}
		return strings.Join(body, "\n"), true
	}
	return "", false
}

// parenthesised returns the text inside the first full-width （...） that
// follows the anchor. These documents list a claim's subjects in that form
// ("which packages may do X"), so it is where a claim's name list is read.
func parenthesised(text, anchor string) (string, bool) {
	idx := strings.Index(text, anchor)
	if idx < 0 {
		return "", false
	}
	rest := text[idx:]
	open := strings.Index(rest, "（")
	if open < 0 {
		return "", false
	}
	rest = rest[open+len("（"):]
	close := strings.Index(rest, "）")
	if close < 0 {
		return "", false
	}
	return rest[:close], true
}

// backtickedNames returns the spans a text wraps in backticks, in order. A
// backtick is how these documents mark a literal name as opposed to prose, so
// it is the right reader for "which names does this sentence claim".
func backtickedNames(text string) []string {
	var out []string
	for {
		start := strings.IndexByte(text, '`')
		if start < 0 {
			return out
		}
		rest := text[start+1:]
		end := strings.IndexByte(rest, '`')
		if end < 0 {
			return out
		}
		out = append(out, rest[:end])
		text = rest[end+1:]
	}
}

// numbersAtLineStart returns the single-digit codes a block lists at the start
// of its lines.
func numbersAtLineStart(block string) []string {
	var out []string
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || trimmed[0] < '0' || trimmed[0] > '9' {
			continue
		}
		if len(trimmed) > 1 && trimmed[1] >= '0' && trimmed[1] <= '9' {
			continue
		}
		out = append(out, string(trimmed[0]))
	}
	return out
}

// tableCodes returns the codes contracts.md lists in a `| \`N\` |` table row.
func tableCodes(doc string) []string {
	var out []string
	for _, m := range reTableCode.FindAllStringSubmatch(doc, -1) {
		out = append(out, m[1])
	}
	return out
}

// artifactPathsAtLineStart returns the audit artifact paths a document lists at
// the start of its lines (leading whitespace allowed).
func artifactPathsAtLineStart(doc string) []string {
	const prefix = `%LOCALAPPDATA%\Lenovo\DriverInstaller\lenovo_driver_`
	var out []string
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		rest, found := strings.CutPrefix(trimmed, prefix)
		if !found {
			continue
		}
		name := cutAtWhitespace(rest)
		if hasArtifactExt(name) {
			out = append(out, prefix+name)
		}
	}
	return out
}

// artifactNamesIn returns every artifact filename a text mentions, with or
// without the directory prefix. The WPF layer spells both.
func artifactNamesIn(text string) []string {
	var out []string
	for i := 0; i+len("lenovo_driver_") <= len(text); i++ {
		if !strings.HasPrefix(text[i:], "lenovo_driver_") {
			continue
		}
		name := cutAtWhitespace(text[i:])
		name = cutAtQuote(name)
		if hasArtifactExt(name) {
			out = append(out, name)
		}
	}
	return out
}

func cutAtWhitespace(s string) string {
	if idx := strings.IndexAny(s, " \t\r\n"); idx >= 0 {
		return s[:idx]
	}
	return s
}

func cutAtQuote(s string) string {
	if idx := strings.IndexAny(s, "`\"'"); idx >= 0 {
		return s[:idx]
	}
	return s
}

func hasArtifactExt(name string) bool {
	for _, ext := range []string{".log", ".csv", ".txt", ".json"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

func toSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, v := range values {
		out[v] = true
	}
	return out
}

// compareSets reports the symmetric difference of two sets, naming each side so
// a failure says which copy has to move.
func compareSets(what string, want, got map[string]bool) error {
	var onlyWant, onlyGot []string
	for name := range want {
		if !got[name] {
			onlyWant = append(onlyWant, name)
		}
	}
	for name := range got {
		if !want[name] {
			onlyGot = append(onlyGot, name)
		}
	}
	if len(onlyWant) == 0 && len(onlyGot) == 0 {
		return nil
	}
	parts := []string{}
	if len(onlyWant) > 0 {
		parts = append(parts, "engine only: "+strings.Join(sorted(onlyWant), ", "))
	}
	if len(onlyGot) > 0 {
		parts = append(parts, "docs only: "+strings.Join(sorted(onlyGot), ", "))
	}
	return fmt.Errorf("%s drift: %s", what, strings.Join(parts, "; "))
}

func dedupe(values []string) []string {
	return slices.Compact(sorted(values))
}

// allCalls returns every call expression in a file.
