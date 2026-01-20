# Matcha Token Generator Usage

## English
- Purpose: convert `vocab_tts.txt` from the Matcha release into `tokens.txt` by phonemizing English entries (Kokoro rules) and generating tone-marked pinyin for Chinese entries.
- Dependencies:
  - `phonemizer`
  - `pypinyin`
  Install once with:
  ```bash
  pip install --user --break-system-packages phonemizer pypinyin
  ```
- Generate tokens:
  ```bash
  python3 scripts/build_matcha_tokens.py \
    --vocab models/matcha_tts_zh_en_20251010/vocab_tts.txt \
    --output models/matcha_tts_zh_en_20251010/tokens.txt
  ```
- Regenerate whenever the upstream vocab changes or you add custom lexicon entries.

## 中文
- 功能：根据 Matcha 发布包中的 `vocab_tts.txt` 自动生成 `tokens.txt`，对英文词条使用 Kokoro 规则转为音素，对中文词条使用拼音（含声调数字）。
- 依赖：
  - `phonemizer`
  - `pypinyin`
  一次性安装：
  ```bash
  pip install --user --break-system-packages phonemizer pypinyin
  ```
- 生成映射：
  ```bash
  python3 scripts/build_matcha_tokens.py \
    --vocab models/matcha_tts_zh_en_20251010/vocab_tts.txt \
    --output models/matcha_tts_zh_en_20251010/tokens.txt
  ```
- 当词表更新或新增自定义词条时，重复执行该命令即可。
