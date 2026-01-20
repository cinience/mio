// Package textfilter provides utilities to analyze and filter text content.
// It can be used to determine if a piece of text is suitable for certain
// processing steps like Text-to-Speech (TTS) synthesis.
package textfilter

import (
	"regexp"
	"strings"
	"unicode"

	streamseg "backend-server/internal/shared/segmentation/streaming"
)

// Constants representing the reasons why text might be unsuitable for TTS.
const (
	ReasonEmptyOrWhitespace = "EMPTY_OR_WHITESPACE"
	ReasonSymbolOrEmojiOnly = "SYMBOL_OR_EMOJI_ONLY"
	ReasonIsURL             = "IS_URL"
	ReasonIsCodeOrMarkup    = "IS_CODE_OR_MARKUP"
	ReasonIsGibberish       = "IS_GIBBERISH"
	ReasonLongNumber        = "LONG_NUMBER"
)

/*
什么样的文本不需要用TTS合成？
空或仅空白内容: 最基本的情况，没有可读内容。
纯粹的标点、符号或Emoji: 如 “！！！？？？”、:-) ^^ 或 🚀✨🎉。虽然某些TTS能读出Emoji的描述，但一长串这样的内容通常是装饰性或情感性的，没有叙述价值。
源代码或标记语言: 如 func main() { ... }、<div class="main"> 或 {"key": "value"}。读出这些内容对用户来说是无用且混乱的。
URL或文件路径: 读出 https://example.com/path/to/resource 体验很差。
无意义的乱码或ID: 如 “asdflkjh”、“trtrtrtrtr” 或一长串哈希值 “b3d29a12b4e1a7b0...”。
超短的、非单词的文本: 如单个字母 "c" 或 "r"（在聊天中常见），除非上下文明确，否则通常没有合成的价值。
纯数字（且过长）: 如银行卡号、订单ID “202510119876543210”。读出来冗长且信息密度低。
*/

// Pre-compiled regular expressions for performance.
var (
	// A simple regex to detect URLs.
	urlRegex = regexp.MustCompile(`(?i)\b(https?|ftp|file)://[-A-Z0-9+&@#/%?=~_|!:,.;]*[-A-Z0-9+&@#/%=~_|]`)

	// A heuristic regex to detect code snippets, JSON, or XML/HTML.
	codeMarkupRegex = regexp.MustCompile(`(?s)(\{[^{}]*:[^{}]*\}|<[^>]+>.*?</[^>]+>|\b(func|var|const|let|class|import|require)\b|=>|->|::|//|/\*)`)

	// A regex to detect long sequences of numbers that are likely IDs.
	longNumberRegex = regexp.MustCompile(`^\d{12,}$`) // 12 or more digits
)

// IsUnspeakable checks if a given text is unsuitable for Text-to-Speech synthesis.
// It returns true if the text should be skipped, along with a machine-readable reason constant.
func IsUnspeakable(text string) (bool, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return true, ReasonEmptyOrWhitespace
	}

	sentences := splitSentences(trimmed)
	if len(sentences) == 0 {
		sentences = []string{trimmed}
	}

	var firstReason string
	hasSpeakable := false

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		unspeakable, reason := analyzeSentence(sentence)
		if !unspeakable {
			hasSpeakable = true
			break
		}
		if firstReason == "" {
			firstReason = reason
		}
	}

	if hasSpeakable {
		return false, ""
	}

	if firstReason == "" {
		firstReason = ReasonEmptyOrWhitespace
	}
	return true, firstReason
}

// isOnlySymbolsOrEmoji checks if a string consists exclusively of characters
// that are not letters or numbers.
func isOnlySymbolsOrEmoji(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return false
		}
	}
	return true
}

// isGibberish applies heuristics to detect nonsensical, unpronounceable text.
func isGibberish(s string) bool {
	totalRunes := []rune(s)
	letterCount := 0
	vowelCount := 0
	asciiLetterCount := 0
	hasNonLatinLetter := false

	for _, r := range totalRunes {
		lower := unicode.ToLower(r)
		if unicode.IsLetter(lower) {
			letterCount++
			if lower <= unicode.MaxASCII {
				asciiLetterCount++
				if strings.ContainsRune("aeiou", lower) {
					vowelCount++
				}
			} else {
				hasNonLatinLetter = true
			}
		}
	}

	if asciiLetterCount > 5 && float64(vowelCount)/float64(asciiLetterCount) < 0.1 {
		return true
	}

	if hasNonLatinLetter && asciiLetterCount == 0 {
		return false
	}

	if len(totalRunes) > 5 {
		charMap := make(map[rune]int)
		for _, r := range totalRunes {
			charMap[r]++
		}
		for _, count := range charMap {
			if float64(count)/float64(len(totalRunes)) > 0.7 {
				return true
			}
		}
	}

	return false
}

func analyzeSentence(sentence string) (bool, string) {
	if sentence == "" {
		return true, ReasonEmptyOrWhitespace
	}

	if isPureURL(sentence) {
		return true, ReasonIsURL
	}

	cleanSentence := strings.TrimSpace(urlRegex.ReplaceAllString(sentence, " "))
	if cleanSentence == "" {
		return true, ReasonIsURL
	}

	switch {
	case looksLikeDecorativeFace(cleanSentence):
		return true, ReasonSymbolOrEmojiOnly
	case codeMarkupRegex.MatchString(cleanSentence):
		return true, ReasonIsCodeOrMarkup
	case longNumberRegex.MatchString(strings.ReplaceAll(cleanSentence, " ", "")):
		return true, ReasonLongNumber
	case isOnlySymbolsOrEmoji(cleanSentence):
		return true, ReasonSymbolOrEmojiOnly
	case isGibberish(cleanSentence):
		return true, ReasonIsGibberish
	default:
		return false, ""
	}
}

func splitSentences(text string) []string {
	if text == "" {
		return nil
	}

	streamer := streamseg.NewSentencizerStreamer(1, 256, 80)
	sentences := streamer.Append(text)
	final, remainder := streamer.Flush()
	sentences = append(sentences, final...)

	if trimmed := strings.TrimSpace(remainder); trimmed != "" {
		sentences = append(sentences, trimmed)
	}

	out := make([]string, 0, len(sentences))
	seen := make(map[string]struct{}, len(sentences))
	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		// Deduplicate consecutive duplicates to avoid redundant checks.
		if _, ok := seen[sentence]; ok {
			continue
		}
		seen[sentence] = struct{}{}
		out = append(out, sentence)
	}

	return out
}

func isPureURL(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	loc := urlRegex.FindStringIndex(trimmed)
	if loc == nil {
		return false
	}
	return loc[0] == 0 && loc[1] == len(trimmed)
}

// FilterSpeakable returns a string that only contains speakable parts of the input.
// Sentences deemed unspeakable are removed. If nothing remains, an empty string is returned.
func FilterSpeakable(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}

	sentences := splitSentences(trimmed)
	if len(sentences) == 0 {
		sentences = []string{trimmed}
	}

	speakable := make([]string, 0, len(sentences))
	var lastReason string
	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		cleaned := cleanSpeakableSentence(sentence)
		if cleaned == "" {
			continue
		}
		if unspeakable, reason := analyzeSentence(cleaned); !unspeakable {
			speakable = append(speakable, cleaned)
		} else {
			lastReason = reason
		}
	}

	if len(speakable) == 0 {
		if lastReason == ReasonIsURL {
			return strings.TrimSpace(urlRegex.ReplaceAllString(trimmed, " "))
		}
		return ""
	}

	return strings.Join(speakable, " ")
}

func cleanSpeakableSentence(sentence string) string {
	if sentence == "" {
		return ""
	}

	// Remove URLs but keep surrounding context.
	cleaned := strings.TrimSpace(urlRegex.ReplaceAllString(sentence, " "))
	if cleaned == "" {
		return ""
	}
	cleaned = strings.TrimSpace(codeMarkupRegex.ReplaceAllString(cleaned, " "))
	if cleaned == "" {
		return ""
	}
	cleaned = removeDecorativeSymbols(cleaned)
	if cleaned == "" {
		return ""
	}

	tokens := strings.Fields(cleaned)
	if len(tokens) == 0 {
		return ""
	}

	// For a single token, return directly if speakable.
	if len(tokens) == 1 {
		token := tokens[0]
		if unspeakable, _ := analyzeSentence(token); unspeakable {
			return ""
		}
		return token
	}

	kept := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if unspeakable, reason := analyzeSentence(token); unspeakable {
			switch reason {
			case ReasonSymbolOrEmojiOnly, ReasonIsURL, ReasonLongNumber, ReasonIsCodeOrMarkup:
				continue
			default:
				return ""
			}
		} else if isLikelyCodeToken(token) {
			continue
		}
		kept = append(kept, token)
	}

	if len(kept) == 0 {
		return ""
	}

	return strings.Join(kept, " ")
}

func isLikelyCodeToken(token string) bool {
	if token == "" {
		return false
	}
	if strings.ContainsAny(token, "{}[]<>") {
		return true
	}
	if strings.Contains(token, "(") || strings.Contains(token, ")") {
		return true
	}
	if strings.Contains(token, "::") || strings.Contains(token, "=>") || strings.Contains(token, "->") {
		return true
	}
	if strings.HasPrefix(token, "#") || strings.HasPrefix(token, "//") {
		return true
	}
	return false
}

func removeDecorativeSymbols(text string) string {
	if text == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(text))
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsPunct(r) || unicode.IsSpace(r) ||
			unicode.In(r, unicode.Sm, unicode.Sk, unicode.Sc) {
			// Preserve math, modifier, and currency symbols so expressions like 1+1=2 survive filtering.
			builder.WriteRune(r)
		}
	}
	return strings.TrimSpace(builder.String())
}

func looksLikeDecorativeFace(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}

	runes := []rune(trimmed)
	total := 0
	bracketCount := 0
	symbolOrPunctCount := 0
	nonBracketSymbolCount := 0
	letterCount := 0
	digitCount := 0
	markCount := 0
	otherCount := 0
	maxLetterDigitRun := 0
	currentRun := 0
	uniqueLetterDigits := make(map[rune]struct{})

	for _, r := range runes {
		if unicode.IsSpace(r) {
			currentRun = 0
			continue
		}

		total++

		switch {
		case unicode.In(r, unicode.Ps, unicode.Pe, unicode.Pi, unicode.Pf):
			bracketCount++
			symbolOrPunctCount++
			currentRun = 0
		case unicode.IsLetter(r):
			letterCount++
			uniqueLetterDigits[r] = struct{}{}
			currentRun++
			if currentRun > maxLetterDigitRun {
				maxLetterDigitRun = currentRun
			}
		case unicode.IsNumber(r):
			digitCount++
			uniqueLetterDigits[r] = struct{}{}
			currentRun++
			if currentRun > maxLetterDigitRun {
				maxLetterDigitRun = currentRun
			}
		case unicode.IsMark(r):
			markCount++
			if currentRun > maxLetterDigitRun {
				maxLetterDigitRun = currentRun
			}
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			symbolOrPunctCount++
			nonBracketSymbolCount++
			currentRun = 0
		default:
			otherCount++
			currentRun = 0
		}
	}

	if total < 3 || total > 30 {
		return false
	}

	lettersDigits := letterCount + digitCount
	if symbolOrPunctCount == 0 {
		return false
	}

	if nonBracketSymbolCount == 0 && markCount == 0 {
		return false
	}

	if lettersDigits == 0 {
		return true
	}

	uniqueLetterDigitCount := len(uniqueLetterDigits)

	if lettersDigits > 6 {
		return false
	}

	if lettersDigits > 4 && uniqueLetterDigitCount > 2 {
		return false
	}

	if lettersDigits >= symbolOrPunctCount && markCount == 0 {
		return false
	}

	if maxLetterDigitRun >= 3 {
		return false
	}

	if otherCount > 0 {
		return false
	}

	return true
}
