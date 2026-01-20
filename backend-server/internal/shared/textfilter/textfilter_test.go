package textfilter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsUnspeakableBasicCases(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		wantUnspeak bool
		wantReason  string
	}{
		{
			name:        "empty text",
			text:        "   ",
			wantUnspeak: true,
			wantReason:  ReasonEmptyOrWhitespace,
		},
		{
			name:        "plain sentence",
			text:        "你好，世界！",
			wantUnspeak: false,
		},
		{
			name:        "pure url",
			text:        "https://example.com/page",
			wantUnspeak: true,
			wantReason:  ReasonIsURL,
		},
		{
			name:        "url with context",
			text:        "请访问 https://example.com 获取更多信息。",
			wantUnspeak: false,
		},
		{
			name:        "emoji only",
			text:        "🚀✨🎉",
			wantUnspeak: true,
			wantReason:  ReasonSymbolOrEmojiOnly,
		},
		{
			name:        "code snippet",
			text:        "func main() { fmt.Println(\"hello\") }",
			wantUnspeak: true,
			wantReason:  ReasonIsCodeOrMarkup,
		},
		{
			name:        "gibberish",
			text:        "sdfghjklqwrty",
			wantUnspeak: true,
			wantReason:  ReasonIsGibberish,
		},
		{
			name:        "mixed sentences with symbols",
			text:        "？？？ 啊，我懂了！",
			wantUnspeak: false,
		},
		{
			name:        "kaomoji face",
			text:        "(๑>ᴗ<๑)",
			wantUnspeak: true,
			wantReason:  ReasonSymbolOrEmojiOnly,
		},
		{
			name:        "table flip kaomoji",
			text:        "(╯°□°）╯︵ ┻━┻",
			wantUnspeak: true,
			wantReason:  ReasonSymbolOrEmojiOnly,
		},
		{
			name:        "complex kaomoji with marks",
			text:        "(๑•̀ㅂ•́)و✧",
			wantUnspeak: true,
			wantReason:  ReasonSymbolOrEmojiOnly,
		},
		{
			name:        "long number only",
			text:        "12345678901234",
			wantUnspeak: true,
			wantReason:  ReasonLongNumber,
		},
		{
			name:        "sentence with decorative face suffix",
			text:        "今天真好！ (๑>ᴗ<๑)",
			wantUnspeak: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unspeakable, reason := IsUnspeakable(tt.text)
			assert.Equal(t, tt.wantUnspeak, unspeakable)
			if tt.wantReason != "" {
				assert.Equal(t, tt.wantReason, reason)
			}
			if !tt.wantUnspeak {
				assert.Empty(t, reason)
			}
		})
	}
}

func TestFilterSpeakable(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "all unspeakable",
			text: "🚀✨🎉",
			want: "",
		},
		{
			name: "mix speakable",
			text: "🚀✨🎉 你好，世界！ https://example.com/path",
			want: "你好，世界！",
		},
		{
			name: "sentence with inline emoji",
			text: "（涂鸦本角落画了只戴齿轮壳的蜗牛，触角闪着小灯💡）",
			want: "（涂鸦本角落画了只戴齿轮壳的蜗牛，触角闪着小灯）",
		},
		{
			name: "code and sentence",
			text: "func main() { fmt.Println(\"hi\") }\n请稍等，我来查一下。",
			want: "请稍等，我来查一下。",
		},
		{
			name: "already clean",
			text: "你好，世界！欢迎回来。",
			want: "你好，世界！ 欢迎回来。",
		},
		{
			name: "math expressions retained",
			text: "1+3是4，4×6是24，10÷2=5。",
			want: "1+3是4，4×6是24，10÷2=5。",
		},
		{
			name: "caret and currency symbols retained",
			text: "2^3=8，同时价格为€5.99。",
			want: "2^3=8，同时价格为€5.99。",
		},
		{
			name: "decorative face removed",
			text: "今天真好 (๑>ᴗ<๑)",
			want: "今天真好",
		},
		{
			name: "decorative face removed in middle",
			text: "你好！ (๑>ᴗ<๑) 记得带伞。",
			want: "你好！ 记得带伞。",
		},
		{
			name: "table flip removed",
			text: "先冷静一下 (╯°□°）╯︵ ┻━┻",
			want: "先冷静一下",
		},
		{
			name: "complex kaomoji removed",
			text: "加油！(๑•̀ㅂ•́)و✧",
			want: "加油！",
		},
		{
			name: "url trimmed but keep context",
			text: "更多信息请访问 https://example.com/docs 获取帮助",
			want: "更多信息请访问 获取帮助",
		},
		{
			name: "long number removed but context kept",
			text: "订单ID 1234567890123456 已生成",
			want: "订单ID 已生成",
		},
		{
			name: "ascii smile skipped",
			text: "Alright :) we go",
			want: "Alright we go",
		},
		{
			name: "url only becomes empty",
			text: "https://example.com/path",
			want: "",
		},
		{
			name: "empty after trim",
			text: "   ",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FilterSpeakable(tt.text))
		})
	}
}

func TestLooksLikeDecorativeFace(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{input: "(๑>ᴗ<๑)", want: true},
		{input: "(*^▽^*)", want: true},
		{input: "(╯°□°）╯︵ ┻━┻", want: true},
		{input: "(๑•̀ㅂ•́)و✧", want: true},
		{input: "｡ﾟ(ﾟ´ω`ﾟ)ﾟ｡", want: true},
		{input: "Hello", want: false},
		{input: "[OK]", want: false},
		{input: "1+1=2", want: false},
	}

	for _, tt := range tests {
		if got := looksLikeDecorativeFace(tt.input); got != tt.want {
			t.Errorf("looksLikeDecorativeFace(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
