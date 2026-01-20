package streaming

import (
	"bytes"
	"strings"
	"unicode"

	sentencizerpkg "github.com/sentencizer/sentencizer"
)

/* SentencizerStreamer
## 分层处理 (Layered Processing):
L1 - 基础分句: 使用一个强大的库（如 sentencizer）来完成“硬边界”的切分，即基于句号、问号、感叹号等明确的句子结束符。这是保证准确性的基础。
L2 - 亚句切分 (Sub-sentence Splitting): 在 L1 分出来的长句子基础上，根据用户配置，在“软边界”（如逗号、分号、破折号）进行二次切分。这是实现“句子尽量短”的关键。
## 状态化与流式处理 (Stateful Streaming):
系统核心是一个带有内部缓冲区的 Segmenter 对象。
它必须实现 io.Writer 接口，这使得它可以与 Go 的标准 I/O（如 io.Copy、http.Request.Body）无缝集成，非常优雅。
它提供一个 Sentences() 或 GetSentences() 方法来拉取当前已处理好的完整句子。
## 可配置性 (Configurable):
“不影响原意”是一个主观标准。因此，我们不能硬编码切分规则。
通过一个 Options 结构体，用户可以精确控制：
在哪些“软标点”上切分。
切分出的最短片段长度，避免产生如 然后、但是 这样无意义的超短句。
*/

type SentencizerStreamer struct {
	zh             sentencizerpkg.Segmenter
	en             sentencizerpkg.Segmenter
	buffer         bytes.Buffer
	minLen         int
	maxLen         int
	splitThreshold int
	isFirst        bool
}

const (
	strongSeparatorLookahead = 24
	softSeparatorMinEmitLen  = 6
)

func NewSentencizerStreamer(minLen, maxLen, splitThreshold int) *SentencizerStreamer {
	return &SentencizerStreamer{
		zh:             sentencizerpkg.NewSegmenter("zh"),
		en:             sentencizerpkg.NewSegmenter("en"),
		minLen:         minLen,
		maxLen:         maxLen,
		splitThreshold: splitThreshold,
		isFirst:        true,
	}
}

func (s *SentencizerStreamer) Append(text string) []string {
	if text == "" {
		return nil
	}
	s.buffer.WriteString(text)
	sentences, remaining := s.extract(s.buffer.String(), s.isFirst, false)

	s.buffer.Reset()
	if remaining != "" {
		s.buffer.WriteString(remaining)
	}

	if len(sentences) > 0 {
		s.isFirst = false
	}

	return sentences
}

func (s *SentencizerStreamer) Flush() ([]string, string) {
	if s.buffer.Len() == 0 {
		return nil, ""
	}

	sentences, remaining := s.extract(s.buffer.String(), s.isFirst, true)

	s.buffer.Reset()
	if len(sentences) > 0 {
		s.isFirst = false
	}

	return sentences, strings.TrimSpace(remaining)
}

func (s *SentencizerStreamer) extract(text string, isFirst bool, forceFlush bool) ([]string, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, ""
	}

	segments := s.segment(trimmed)
	if len(segments) == 0 {
		return nil, trimmed
	}

	ctx := newSplitContext(isFirst, forceFlush)
	for idx, segment := range segments {
		s.consumeSegment(&ctx, segment, idx == len(segments)-1)
	}

	return ctx.sentences, ctx.remainder.String()
}

func (s *SentencizerStreamer) segment(text string) []string {
	segmenter := s.selectSegmenter(text)
	spans := segmenter.TextSpans(text)
	if len(spans) == 0 {
		return nil
	}

	segments := make([]string, 0, len(spans))
	for _, span := range spans {
		if sentence := strings.TrimSpace(span.Sentence); sentence != "" {
			segments = append(segments, sentence)
		}
	}
	return segments
}

type splitContext struct {
	sentences  []string
	remainder  strings.Builder
	separator  map[rune]struct{}
	forceFlush bool
}

func newSplitContext(isFirst bool, forceFlush bool) splitContext {
	separator := punctuationMap
	if isFirst {
		separator = firstPunctuation
	}
	return splitContext{
		separator:  separator,
		forceFlush: forceFlush,
	}
}

func (ctx *splitContext) addSentence(sentence string) {
	ctx.sentences = append(ctx.sentences, sentence)
}

func (ctx *splitContext) appendRemainder(segment string) {
	if segment == "" {
		return
	}
	if ctx.remainder.Len() > 0 {
		ctx.remainder.WriteRune(' ')
	}
	ctx.remainder.WriteString(segment)
}

func (ctx *splitContext) hasSentences() bool {
	return len(ctx.sentences) > 0
}

type pendingSegment struct {
	text           string
	isLast         bool
	forceEmit      bool
	thresholdSplit bool
}

func (s *SentencizerStreamer) consumeSegment(ctx *splitContext, segment string, isLast bool) {
	queue := s.prepareSegments(segment, isLast, !ctx.hasSentences())
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		text := strings.TrimSpace(current.text)
		if text == "" {
			continue
		}

		runes := []rune(text)
		if !current.forceEmit && s.splitThreshold > 0 && len(runes) > s.splitThreshold {
			if !ctx.forceFlush && !containsStrongSeparator(text) && len(runes) <= s.splitThreshold+softSeparatorMinEmitLen {
				ctx.appendRemainder(text)
				continue
			}
			subSentences, subRemaining := extractSmartSentencesWithLimit(text, s.minLen, s.splitThreshold)
			trimmedRemaining := strings.TrimSpace(subRemaining)

			if len(subSentences) > 1 || (len(subSentences) == 1 && strings.TrimSpace(subSentences[0]) != text) || trimmedRemaining != "" {
				for idx, sub := range subSentences {
					trimmedSub := strings.TrimSpace(sub)
					if trimmedSub == "" {
						continue
					}
					subRunes := []rune(trimmedSub)
					force := ctx.forceFlush
					if !force && len(subRunes) > 0 {
						last := subRunes[len(subRunes)-1]
						if _, ok := strongSeparators[last]; ok {
							force = true
						} else if _, ok := softSeparators[last]; ok {
							if len(subRunes) > softSeparatorMinEmitLen {
								force = true
							}
						} else if _, ok := fallbackSeparators[last]; ok {
							if len(subRunes) > softSeparatorMinEmitLen {
								force = true
							}
						}
					}
					queue = append(queue, pendingSegment{
						text:           trimmedSub,
						isLast:         current.isLast && idx == len(subSentences)-1 && trimmedRemaining == "",
						forceEmit:      force,
						thresholdSplit: true,
					})
				}
				if trimmedRemaining != "" {
					queue = append(queue, pendingSegment{
						text:           trimmedRemaining,
						isLast:         current.isLast,
						forceEmit:      ctx.forceFlush,
						thresholdSplit: true,
					})
				}
				continue
			}
		}

		if !current.forceEmit && !ctx.forceFlush && current.isLast && shouldDefer(text) {
			ctx.appendRemainder(text)
			continue
		}

		shouldEmit := len(runes) >= s.minLen && endsWithSeparator(runes, ctx.separator)
		if current.thresholdSplit && shouldEmit {
			last := runes[len(runes)-1]
			if _, soft := softSeparators[last]; soft && !ctx.forceFlush {
				if len(runes) <= softSeparatorMinEmitLen {
					shouldEmit = false
				}
			}
		}

		if current.forceEmit || shouldEmit {
			ctx.addSentence(text)
			continue
		}

		ctx.appendRemainder(text)
	}
}

func (s *SentencizerStreamer) prepareSegments(segment string, isLast bool, treatAsFirst bool) []pendingSegment {
	trimmed := strings.TrimSpace(segment)
	if trimmed == "" {
		return nil
	}

	runes := []rune(trimmed)
	if s.maxLen > 0 && len(runes) > s.maxLen {
		fallbackSentences, fallbackRemaining := extractSmartSentences(trimmed, s.minLen, s.maxLen, treatAsFirst)
		segments := make([]pendingSegment, 0, len(fallbackSentences)+1)
		trimmedRemaining := strings.TrimSpace(fallbackRemaining)

		for idx, sentence := range fallbackSentences {
			segments = append(segments, pendingSegment{
				text:   sentence,
				isLast: isLast && idx == len(fallbackSentences)-1 && trimmedRemaining == "",
			})
		}

		if trimmedRemaining != "" {
			segments = append(segments, pendingSegment{
				text:   trimmedRemaining,
				isLast: isLast,
			})
		}

		if len(segments) > 0 {
			return segments
		}
	}

	return []pendingSegment{{text: trimmed, isLast: isLast}}
}

func (s *SentencizerStreamer) selectSegmenter(text string) sentencizerpkg.Segmenter {
	if containsHan(text) {
		return s.zh
	}
	return s.en
}

var (
	punctuationMap = map[rune]struct{}{
		'。':  {},
		'？':  {},
		'！':  {},
		'；':  {},
		'：':  {},
		'\n': {},
		'.':  {},
		'?':  {},
		'!':  {},
		';':  {},
		':':  {},
	}

	firstPunctuation = map[rune]struct{}{
		'，':  {},
		',':  {},
		'。':  {},
		'？':  {},
		'！':  {},
		'；':  {},
		'：':  {},
		'\n': {},
		'.':  {},
		'?':  {},
		'!':  {},
		';':  {},
		':':  {},
	}
)

var fallbackSeparators = map[rune]struct{}{
	'，': {},
	'、': {},
	'…': {},
	'—': {},
	'-': {},
	'(': {},
	')': {},
	'（': {},
	'）': {},
	'【': {},
	'】': {},
	'[': {},
	']': {},
	'「': {},
	'」': {},
	'『': {},
	'』': {},
	'*': {},
	'✨': {},
	'🌟': {},
	'⭐': {},
	'💡': {},
}

var softSeparators = map[rune]struct{}{
	'，': {},
	',': {},
	'、': {},
}

var strongSeparators = map[rune]struct{}{
	'。': {},
	'？': {},
	'！': {},
	'.': {},
	'?': {},
	'!': {},
	'；': {},
	';': {},
	'：': {},
	':': {},
}

func extractSmartSentences(text string, minLen, maxLen int, isFirst bool) (sentences []string, remaining string) {
	separatorMap := punctuationMap
	if isFirst {
		separatorMap = firstPunctuation
	}

	runes := []rune(text)
	cursor := 0

	for cursor < len(runes) {
		for cursor < len(runes) && unicode.IsSpace(runes[cursor]) {
			cursor++
		}
		if cursor >= len(runes) {
			break
		}

		splitPos := findNextSplitPoint(runes, cursor, maxLen, separatorMap)
		if splitPos == -1 {
			if pos := findStrongSeparatorWithin(runes, cursor, maxLen+strongSeparatorLookahead); pos != -1 {
				splitPos = pos
			}
		}
		if splitPos == -1 {
			fallbackPos := findFallbackSplitPoint(runes, cursor, maxLen)
			if fallbackPos != -1 {
				segment := trimSpaceRunes(runes[cursor : fallbackPos+1])
				if len(segment) > 0 {
					sentences = append(sentences, string(segment))
				}
				cursor = fallbackPos + 1
				continue
			}

			segment := trimSpaceRunes(runes[cursor:])
			if len(segment) > 0 {
				remaining = string(segment)
			}
			break
		}

		segment := trimSpaceRunes(runes[cursor : splitPos+1])
		switch {
		case len(segment) == 0:
		case len(segment) < minLen:
			sentences = append(sentences, string(segment))
		case hasSeparator(segment[len(segment)-1], separatorMap):
			sentences = append(sentences, string(segment))
		default:
			if len(segment) > 0 {
				if len(remaining) > 0 {
					remaining += " "
				}
				remaining += string(segment)
			}
		}

		cursor = splitPos + 1
	}

	return sentences, remaining
}

func extractSmartSentencesWithLimit(text string, minLen, maxLen int) ([]string, string) {
	return extractSmartSentences(text, minLen, maxLen, true)
}

func findFallbackSplitPoint(text []rune, startPos int, maxLen int) int {
	if maxLen <= 0 {
		return -1
	}
	endPos := startPos + maxLen
	if endPos > len(text) {
		endPos = len(text)
	}

	candidates := []int{}
	for i := startPos; i < endPos; i++ {
		r := text[i]
		if unicode.IsSpace(r) {
			candidates = append(candidates, i)
			continue
		}
		if _, ok := fallbackSeparators[r]; ok {
			candidates = append(candidates, i)
			continue
		}
		if unicode.IsPunct(r) && runeCanBeFallback(r) {
			candidates = append(candidates, i)
		}
	}

	for i := len(candidates) - 1; i >= 0; i-- {
		if candidates[i] > startPos {
			return candidates[i]
		}
	}

	if startPos < len(text) && endPos > startPos {
		return endPos - 1
	}

	return -1
}

func runeCanBeFallback(r rune) bool {
	switch r {
	case '.', '?', '!', ';', ':', '。', '？', '！', '；', '：':
		return false
	}
	return true
}

func hasSeparator(r rune, separatorMap map[rune]struct{}) bool {
	_, ok := separatorMap[r]
	return ok
}

func findStrongSeparatorWithin(text []rune, startPos int, maxLen int) int {
	if maxLen <= 0 {
		return -1
	}
	endPos := startPos + maxLen
	if endPos > len(text) {
		endPos = len(text)
	}
	for i := startPos; i < endPos; i++ {
		r := text[i]
		if _, ok := strongSeparators[r]; !ok {
			continue
		}
		if r == '.' && isDotBetweenDigits(text, i) {
			continue
		}
		if (r == ':' || r == '：') && isColonBetweenDigits(text, i) {
			continue
		}
		return i
	}
	return -1
}

func containsStrongSeparator(text string) bool {
	for _, r := range text {
		if _, ok := strongSeparators[r]; ok {
			return true
		}
	}
	return false
}

func findNextSplitPoint(text []rune, startPos int, maxLen int, separatorMap map[rune]struct{}) int {
	endPos := startPos + maxLen
	if endPos > len(text) {
		endPos = len(text)
	}

	for i := startPos; i < endPos; i++ {
		if text[i] == '\n' {
			nextPos := i + 1
			for nextPos < endPos && unicode.IsSpace(text[nextPos]) {
				nextPos++
			}
			if nextPos < endPos-2 && text[nextPos] >= '0' && text[nextPos] <= '9' {
				return i
			}
			continue
		}

		if _, ok := separatorMap[text[i]]; ok {
			if isDotBetweenDigits(text, i) {
				continue
			}
			if isColonBetweenDigits(text, i) {
				continue
			}
			return i
		}
	}

	return -1
}

func trimSpaceRunes(text []rune) []rune {
	start, end := 0, len(text)-1

	for start <= end && unicode.IsSpace(text[start]) {
		start++
	}

	for end >= start && unicode.IsSpace(text[end]) {
		end--
	}

	if start > end {
		return nil
	}
	return text[start : end+1]
}

func containsHan(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func endsWithSeparator(runes []rune, separatorMap map[rune]struct{}) bool {
	if len(runes) == 0 {
		return false
	}
	last := runes[len(runes)-1]
	if _, ok := separatorMap[last]; !ok {
		return false
	}
	if last == '.' && isDotBetweenDigits(runes, len(runes)-1) {
		return false
	}
	if (last == ':' || last == '：') && isColonBetweenDigits(runes, len(runes)-1) {
		return false
	}
	return true
}

func isDotBetweenDigits(text []rune, pos int) bool {
	if pos < 0 || pos >= len(text) || text[pos] != '.' {
		return false
	}
	if pos == 0 || pos == len(text)-1 {
		return false
	}
	return unicode.IsDigit(text[pos-1]) && unicode.IsDigit(text[pos+1])
}

func isColonBetweenDigits(text []rune, pos int) bool {
	if pos < 0 || pos >= len(text) || (text[pos] != ':' && text[pos] != '：') {
		return false
	}
	if pos == 0 || pos == len(text)-1 {
		return false
	}
	return unicode.IsDigit(text[pos-1]) && unicode.IsDigit(text[pos+1])
}

func shouldDefer(segment string) bool {
	runes := []rune(strings.TrimSpace(segment))
	if len(runes) == 0 {
		return false
	}
	last := runes[len(runes)-1]
	if last == '.' || last == ':' || last == '：' {
		if len(runes) == 1 {
			return true
		}
		return unicode.IsDigit(runes[len(runes)-2])
	}
	return false
}
