package streaming

import (
	"strings"
	"testing"
)

func TestSentencizerStreamer_IPAndDecimals(t *testing.T) {
	streamer := NewSentencizerStreamer(2, 100, 60)

	if sentences := streamer.Append("公共IP是127.0.0."); len(sentences) != 0 {
		t.Fatalf("expected no sentences, got %#v", sentences)
	}

	if sentences := streamer.Append("1，请检查。温度是3."); len(sentences) != 1 || sentences[0] != "公共IP是127.0.0.1，请检查。" {
		t.Fatalf("unexpected sentences: %#v", sentences)
	}

	if sentences := streamer.Append("14度。"); len(sentences) != 1 || sentences[0] != "温度是3.14度。" {
		t.Fatalf("unexpected sentences: %#v", sentences)
	}

	finalSentences, remainder := streamer.Flush()
	if len(finalSentences) != 0 {
		t.Fatalf("expected no final sentences, got %#v", finalSentences)
	}
	if remainder != "" {
		t.Fatalf("expected empty remainder, got %q", remainder)
	}
}

func TestSentencizerStreamer_TimeExpression(t *testing.T) {
	streamer := NewSentencizerStreamer(2, 100, 60)

	if sentences := streamer.Append("现在是北京时间2023年10月6日15:23:"); len(sentences) != 0 {
		t.Fatalf("expected no sentences, got %#v", sentences)
	}

	if sentences := streamer.Append("45啦。"); len(sentences) != 1 || sentences[0] != "现在是北京时间2023年10月6日15:23:45啦。" {
		t.Fatalf("unexpected sentences: %#v", sentences)
	}

	finalSentences, remainder := streamer.Flush()
	if len(finalSentences) != 0 {
		t.Fatalf("expected no final sentences, got %#v", finalSentences)
	}
	if remainder != "" {
		t.Fatalf("expected empty remainder, got %q", remainder)
	}
}
func TestSentencizerStreamer_LongSentenceSplit(t *testing.T) {
	streamer := NewSentencizerStreamer(2, 100, 20)

	longText := "这是一个特别长的句子，包含了很多很多的内容，希望可以被拆分成更短的片段。"
	if sentences := streamer.Append(longText); len(sentences) < 2 {
		t.Fatalf("expected at least 2 sentences, got %#v", sentences)
	}
	finalSentences, remainder := streamer.Flush()
	if len(finalSentences) != 0 {
		t.Fatalf("expected no remaining sentences, got %#v", finalSentences)
	}
	if remainder != "" {
		t.Fatalf("expected empty remainder, got %q", remainder)
	}
}

func TestSentencizerStreamer_FallbackSplitWithEmoji(t *testing.T) {
	streamer := NewSentencizerStreamer(2, 60, 20)

	text := "✨（眼睛亮晶晶地翻开神奇涂鸦本） 我的机器蜗牛刚刚在本子上爬出了一道彩虹轨迹……诶，（笔尖突然发出“叮咚”太空音效） **好奇心碎片+1** 🌟（当前1/5）"
	sentences := streamer.Append(text)
	if len(sentences) < 2 {
		t.Fatalf("expected at least 2 sentences, got %#v", sentences)
	}
	for _, sentence := range sentences {
		if l := len([]rune(sentence)); l > 40 {
			t.Fatalf("expected fallback split to limit length, got sentence length %d: %q", l, sentence)
		}
	}
}

func TestSentencizerStreamer_CJKChunkBoundary(t *testing.T) {
	streamer := NewSentencizerStreamer(2, 100, 30)

	if sentences := streamer.Append("在偷看我男友的Python书啦～他刚刚又送我一把机械键盘当礼"); len(sentences) != 0 {
		t.Fatalf("expected no sentences for first chunk, got %#v", sentences)
	}

	if sentences := streamer.Append("物，真的"); len(sentences) != 0 {
		t.Fatalf("expected no sentences for soft punctuation chunk, got %#v", sentences)
	}

	if sentences := streamer.Append("假的啦！"); len(sentences) != 1 || sentences[0] != "在偷看我男友的Python书啦～他刚刚又送我一把机械键盘当礼物，真的假的啦！" {
		t.Fatalf("unexpected sentences after strong punctuation: %#v", sentences)
	}

	finalSentences, remainder := streamer.Flush()
	if len(finalSentences) != 0 {
		t.Fatalf("expected no remaining sentences after flush, got %#v", finalSentences)
	}
	if remainder != "" {
		t.Fatalf("expected empty remainder, got %q", remainder)
	}
}

func TestSentencizerStreamer_UnpunctuatedFlush(t *testing.T) {
	streamer := NewSentencizerStreamer(2, 100, 30)

	text := "这是一个完全没有任何终止符号但长度超过阈值的描述我们希望在会话结束时整体返回而不是被强行折断"
	if sentences := streamer.Append(text[:30]); len(sentences) != 0 {
		t.Fatalf("expected no sentences for first fragment, got %#v", sentences)
	}
	if sentences := streamer.Append(text[30:]); len(sentences) != 0 {
		t.Fatalf("expected no sentences for second fragment, got %#v", sentences)
	}

	finalSentences, remainder := streamer.Flush()
	if remainder != "" {
		t.Fatalf("expected empty remainder, got %q", remainder)
	}
	if len(finalSentences) == 0 {
		t.Fatalf("expected fallback sentences on flush, got none")
	}
	combined := strings.Join(finalSentences, "")
	if combined != text {
		t.Fatalf("expected concatenated sentences to equal original text, got %q", combined)
	}
}
