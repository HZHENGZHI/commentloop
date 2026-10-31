// Package anchor relocates comment quotes and review sections inside Markdown
// source that agents and users keep rewriting.
//
// Comments are stored with a quote and, when the Agent replies, with the
// before/after excerpts of the edited block. Formatting and surrounding text
// change over time, so every lookup is a best-effort search: an exact match
// first, then a plain-text comparison, then a section fallback. The package
// holds only that text algorithm; persistence, HTTP and comment ordering stay
// in the server package.
package anchor

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	imageRE      = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	linkRE       = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	htmlTagRE    = regexp.MustCompile(`<[^>]+>`)
	listMarkerRE = regexp.MustCompile(`^(?:[-*+]|\d+\.)\s+(?:\[[ xX]\]\s*)?`)
)

// LocateCompact falls back to letters/digits only. It handles rendered
// selections such as "Hello world" matching "**Hello** world" without needing
// a raw source position.
func LocateCompact(content, quote string, nearLine int) (int, bool) {
	needle := make([]rune, 0, len(quote))
	for _, r := range quote {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			needle = append(needle, unicode.ToLower(r))
		}
	}
	if len(needle) < 2 {
		return 0, false
	}
	haystack := make([]rune, 0, len(content))
	lineOf := make([]int, 0, len(content))
	line := 1
	for _, r := range content {
		if r == '\n' {
			line++
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			haystack = append(haystack, unicode.ToLower(r))
			lineOf = append(lineOf, line)
		}
	}
	bestLine := 0
	bestDistance := int(^uint(0) >> 1)
	for i := 0; i+len(needle) <= len(haystack); i++ {
		matches := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		candidateLine := lineOf[i]
		if nearLine <= 0 {
			return candidateLine, true
		}
		distance := candidateLine - nearLine
		if distance < 0 {
			distance = -distance
		}
		if distance < bestDistance {
			bestDistance = distance
			bestLine = candidateLine
		}
	}
	if bestLine == 0 {
		return 0, false
	}
	return bestLine, true
}

// PlainText converts a Markdown excerpt into a plain-text anchor that can be
// relocated in the rendered preview after an agent edit.
func PlainText(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") {
			continue
		}
		line = imageRE.ReplaceAllString(line, "$1")
		line = linkRE.ReplaceAllString(line, "$1")
		line = htmlTagRE.ReplaceAllString(line, "")
		line = listMarkerRE.ReplaceAllString(line, "")
		line = strings.TrimLeft(line, "#> ")
		line = strings.TrimSpace(line)
		line = strings.Trim(line, "`*_~")
		line = strings.ReplaceAll(line, "`", "")
		cleaned = append(cleaned, line)
	}
	text := strings.Join(cleaned, "\n")
	paragraphs := strings.Split(text, "\n\n")
	best := ""
	bestLen := 0
	for _, paragraph := range paragraphs {
		paragraph = strings.Join(strings.Fields(paragraph), " ")
		length := len([]rune(paragraph))
		if length > bestLen {
			best, bestLen = paragraph, length
		}
	}
	if bestLen > 400 {
		best = string([]rune(best)[:400])
	}
	return strings.TrimSpace(best)
}

// LocateQuote maps a (possibly rendered/plain-text) quote back to the Markdown
// source. Prefix/suffix help with duplicate text; the stored line is a fallback.
func LocateQuote(content, quote, prefix, suffix string, nearLine int) (start, end, line int, orphaned bool) {
	if content == "" || quote == "" {
		return 0, 0, 0, true
	}
	all := make([]int, 0, 2)
	offset := 0
	for {
		index := strings.Index(content[offset:], quote)
		if index < 0 {
			break
		}
		all = append(all, offset+index)
		offset += index + maxInt(1, len(quote))
		if len(all) > 200 {
			break
		}
	}
	if len(all) == 0 {
		return 0, 0, 0, true
	}
	best := all[0]
	bestScore := -1
	for _, index := range all {
		lineNo := 1 + strings.Count(content[:index], "\n")
		score := 0
		if nearLine > 0 {
			distance := lineNo - nearLine
			if distance < 0 {
				distance = -distance
			}
			score -= distance
		}
		if prefix != "" {
			startPrefix := index - len(prefix)
			if startPrefix >= 0 && content[startPrefix:index] == prefix {
				score += 20
			}
		}
		if suffix != "" {
			endSuffix := index + len(quote) + len(suffix)
			if endSuffix <= len(content) && content[index+len(quote):endSuffix] == suffix {
				score += 20
			}
		}
		if score > bestScore {
			best, bestScore = index, score
		}
	}
	return best, best + len(quote), 1 + strings.Count(content[:best], "\n"), false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// SectionRange returns the byte range of the blank-line-delimited Markdown
// block containing marker (an agent after excerpt) or, as a fallback, line.
// The block never includes the surrounding blank lines.
func SectionRange(content, marker, quote string, line int) (int, int, bool) {
	if content == "" {
		return 0, 0, false
	}
	index := -1
	for _, candidate := range []string{strings.TrimSpace(marker), strings.TrimSpace(quote)} {
		if candidate == "" {
			continue
		}
		if at := strings.Index(content, candidate); at >= 0 {
			index = at
			break
		}
	}
	if index < 0 {
		lines := strings.Split(content, "\n")
		if len(lines) == 0 {
			return 0, 0, false
		}
		lineNo := line
		if lineNo < 1 {
			lineNo = 1
		}
		if lineNo > len(lines) {
			lineNo = len(lines)
		}
		chosen := lineNo - 1
		if strings.TrimSpace(lines[chosen]) == "" {
			found := -1
			for delta := 1; delta < len(lines); delta++ {
				if next := chosen + delta; next < len(lines) && strings.TrimSpace(lines[next]) != "" {
					found = next
					break
				}
				if prev := chosen - delta; prev >= 0 && strings.TrimSpace(lines[prev]) != "" {
					found = prev
					break
				}
			}
			if found < 0 {
				return 0, 0, false
			}
			chosen = found
		}
		index = 0
		for i := 0; i < chosen; i++ {
			index += len(lines[i]) + 1
		}
	}
	return expandSection(content, index)
}

func expandSection(content string, index int) (int, int, bool) {
	if index < 0 || index >= len(content) {
		return 0, 0, false
	}
	lineStart := strings.LastIndex(content[:index], "\n") + 1
	start := lineStart
	for start > 0 {
		previousEnd := start - 1
		previousStart := strings.LastIndex(content[:previousEnd], "\n") + 1
		if strings.TrimSpace(content[previousStart:previousEnd]) == "" {
			break
		}
		start = previousStart
	}
	end := len(content)
	if relative := strings.Index(content[index:], "\n"); relative >= 0 {
		end = index + relative
	}
	for end < len(content) {
		nextStart := end + 1
		if nextStart >= len(content) {
			end = len(content)
			break
		}
		nextEnd := len(content)
		if relative := strings.Index(content[nextStart:], "\n"); relative >= 0 {
			nextEnd = nextStart + relative
		}
		if strings.TrimSpace(content[nextStart:nextEnd]) == "" {
			break
		}
		end = nextEnd
	}
	for end > start && (content[end-1] == '\n' || content[end-1] == '\r') {
		end--
	}
	if end <= start {
		return 0, 0, false
	}
	return start, end, true
}

// SectionFromVersion returns the section an Agent revision touched. Stored byte
// offsets win; when they are missing, the excerpt is located like a quote.
func SectionFromVersion(content, after string, sectionStart, sectionEnd int, quote string, line int) (string, bool) {
	if sectionStart >= 0 && sectionEnd > sectionStart && sectionEnd <= len(content) {
		if text := strings.TrimRight(content[sectionStart:sectionEnd], "\r\n"); strings.TrimSpace(text) != "" {
			return text, true
		}
	}
	start, end, ok := SectionRange(content, after, quote, line)
	if !ok {
		return "", false
	}
	return strings.TrimRight(content[start:end], "\r\n"), true
}

// ReplaceSection replaces one blank-line-delimited block. When the same block
// text appears more than once, the closest line wins.
func ReplaceSection(content, current, target string, line int) (string, bool) {
	if current == "" {
		return content, false
	}
	positions := make([]int, 0, 1)
	offset := 0
	for {
		index := strings.Index(content[offset:], current)
		if index < 0 {
			break
		}
		positions = append(positions, offset+index)
		offset += index + len(current)
	}
	if len(positions) == 0 {
		return content, false
	}
	chosen := positions[0]
	if line > 0 && len(positions) > 1 {
		bestDistance := int(^uint(0) >> 1)
		for _, position := range positions {
			distance := strings.Count(content[:position], "\n") + 1 - line
			if distance < 0 {
				distance = -distance
			}
			if distance < bestDistance {
				bestDistance = distance
				chosen = position
			}
		}
	}
	return content[:chosen] + target + content[chosen+len(current):], true
}
