package main

import "strings"

var bigGlyphs = map[rune][5]string{
	'0': {"###", "#.#", "#.#", "#.#", "###"},
	'1': {".#.", "##.", ".#.", ".#.", "###"},
	'2': {"###", "..#", "###", "#..", "###"},
	'3': {"###", "..#", "###", "..#", "###"},
	'4': {"#.#", "#.#", "###", "..#", "..#"},
	'5': {"###", "#..", "###", "..#", "###"},
	'6': {"###", "#..", "###", "#.#", "###"},
	'7': {"###", "..#", "..#", "..#", "..#"},
	'8': {"###", "#.#", "###", "#.#", "###"},
	'9': {"###", "#.#", "###", "..#", "###"},
	':': {".", "#", ".", "#", "."},
}

func bigClock(text string, compact bool) string {
	var rows [5]strings.Builder
	first := true
	for _, r := range text {
		glyph, ok := bigGlyphs[r]
		if !ok {
			continue
		}
		for i := range rows {
			if !first {
				rows[i].WriteString("  ")
			}
			for _, px := range glyph[i] {
				if px == '#' {
					rows[i].WriteString("██")
				} else {
					rows[i].WriteString("  ")
				}
			}
		}
		first = false
	}

	full := make([]string, len(rows))
	for i := range rows {
		full[i] = rows[i].String()
	}
	if !compact {
		return strings.Join(full, "\n")
	}

	var out []string
	for i := 0; i < len(full); i += 2 {
		top := []rune(full[i])
		var bottom []rune
		if i+1 < len(full) {
			bottom = []rune(full[i+1])
		}
		var b strings.Builder
		for j, tc := range top {
			isTop := tc == '█'
			isBottom := j < len(bottom) && bottom[j] == '█'
			switch {
			case isTop && isBottom:
				b.WriteRune('█')
			case isTop:
				b.WriteRune('▀')
			case isBottom:
				b.WriteRune('▄')
			default:
				b.WriteRune(' ')
			}
		}
		out = append(out, b.String())
	}
	return strings.Join(out, "\n")
}
