package utils

import (
	"strings"
	"sync"
	"unicode"

	"backend-server/internal/config"

	"github.com/mozillazg/go-pinyin"
)

var (
	pinyinArgs = func() pinyin.Args {
		args := pinyin.NewArgs()
		args.Style = pinyin.Normal
		args.Heteronym = false
		return args
	}()
	pinyinCache sync.Map
)

// RemovePunctuation 移除文本中的标点符号
func RemovePunctuation(text string) string {
	// 创建一个字符串构建器
	var builder strings.Builder
	builder.Grow(len(text))

	for _, r := range text {
		if !unicode.IsPunct(r) && !unicode.IsSpace(r) {
			builder.WriteRune(r)
		}
	}

	return builder.String()
}

func normalizeWakeWord(text string) string {
	if text == "" {
		return ""
	}
	lowered := strings.ToLower(text)
	return RemovePunctuation(lowered)
}

// IsWakeupWord 检查文本是否是唤醒词
func IsWakeupWord(text string) bool {
	cfg := config.GetConfig()
	if cfg == nil {
		return false
	}

	normalizedText := normalizeWakeWord(text)
	if normalizedText == "" {
		return false
	}

	for _, word := range cfg.WakeupWords {
		if normalizedText == normalizeWakeWord(word) {
			return true
		}
	}

	matchCfg := cfg.WakeupWordMatch
	if !matchCfg.EnablePinyinFuzzy {
		return false
	}

	inputPinyin := toPinyin(normalizedText)
	if inputPinyin == "" {
		return false
	}

	maxDistance := matchCfg.MaxPinyinDistance
	if maxDistance <= 0 {
		maxDistance = 1
	}

	for _, word := range cfg.WakeupWords {
		candidate := normalizeWakeWord(word)
		if candidate == "" {
			continue
		}
		if levenshteinDistance(inputPinyin, toPinyin(candidate)) <= maxDistance {
			return true
		}
	}

	return false
}

func toPinyin(text string) string {
	if text == "" {
		return ""
	}

	if cached, ok := pinyinCache.Load(text); ok {
		return cached.(string)
	}

	syllables := pinyin.LazyPinyin(text, pinyinArgs)
	if len(syllables) == 0 {
		return ""
	}

	var builder strings.Builder
	for _, syllable := range syllables {
		if syllable == "" {
			continue
		}
		builder.WriteString(syllable)
	}

	result := builder.String()
	pinyinCache.Store(text, result)
	return result
}

func levenshteinDistance(a, b string) int {
	if a == b {
		return 0
	}

	ar := []rune(a)
	br := []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}

	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}

	for i, ra := range ar {
		curr[0] = i + 1
		for j, rb := range br {
			cost := 0
			if ra != rb {
				cost = 1
			}

			deletion := prev[j+1] + 1
			insertion := curr[j] + 1
			substitution := prev[j] + cost

			curr[j+1] = min3(deletion, insertion, substitution)
		}
		copy(prev, curr)
	}

	return prev[len(br)]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}

	if b < c {
		return b
	}
	return c
}
