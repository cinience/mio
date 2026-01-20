package task

import "strings"

func SanitizeLogLine(line string) string {
	if line == "" {
		return ""
	}
	line = stripANSIEscape(line)
	return strings.TrimRight(line, "\r")
}

func stripANSIEscape(input string) string {
	var out []rune
	for i := 0; i < len(input); i++ {
		if input[i] != 0x1b {
			out = append(out, rune(input[i]))
			continue
		}
		if i+1 >= len(input) {
			continue
		}
		next := input[i+1]
		switch next {
		case '[':
			i = skipCSI(input, i+2)
		case ']':
			i = skipOSC(input, i+2)
		case 'P':
			i = skipDCS(input, i+2)
		default:
			// Skip single ESC + next
			i++
		}
	}
	return string(out)
}

func skipCSI(input string, start int) int {
	i := start
	for i < len(input) {
		b := input[i]
		if (b >= 0x40 && b <= 0x7E) || b == '\n' {
			return i
		}
		i++
	}
	return len(input)
}

func skipOSC(input string, start int) int {
	i := start
	for i < len(input) {
		if input[i] == 0x07 {
			return i
		}
		if input[i] == 0x1b && i+1 < len(input) && input[i+1] == '\\' {
			return i + 1
		}
		i++
	}
	return len(input)
}

func skipDCS(input string, start int) int {
	i := start
	for i < len(input) {
		if input[i] == 0x1b && i+1 < len(input) && input[i+1] == '\\' {
			return i + 1
		}
		i++
	}
	return len(input)
}
