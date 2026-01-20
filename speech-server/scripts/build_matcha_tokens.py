#!/usr/bin/env python3
"""
Generate a Matcha-compatible ``tokens.txt`` mapping from a vocab list.

生成适用于 Matcha 模型的 ``tokens.txt`` 映射工具。

The Matcha TTS release only exposes ``vocab_tts.txt``. English entries need to be
phonemized using Kokoro/IPA rules, while Chinese entries should be converted to
pinyin with tone numbers. This helper mirrors that pipeline and assigns
incremental token ids (starting at 0) while preserving the first-seen ordering.

Matcha 官方包仅提供 ``vocab_tts.txt`` 原始词表：英文部分需按 Kokoro 规则转为音素，
中文部分需借助 ``pypinyin`` 转为带声调的拼音。脚本自动完成上述转换并生成 sherpa-onnx
需要的 ``tokens.txt``（按出现顺序从 0 开始编号）。

Usage / 使用方法::

    python3 scripts/build_matcha_tokens.py \\
      --vocab models/matcha_tts_zh_en_20251010/vocab_tts.txt \\
      --output models/matcha_tts_zh_en_20251010/tokens.txt
"""

from __future__ import annotations

import argparse
from collections.abc import Iterable, Iterator
from pathlib import Path

try:
    from phonemizer import phonemize
except ImportError as exc:  # pragma: no cover - dependency resolution
    raise SystemExit(
        "phonemizer is required. Install via `pip install --user phonemizer`."
    ) from exc

try:
    from pypinyin import Style, lazy_pinyin
except ImportError as exc:  # pragma: no cover - dependency resolution
    raise SystemExit(
        "pypinyin is required. Install via `pip install --user pypinyin`."
    ) from exc


def _contains_cjk(text: str) -> bool:
    return any("\u4e00" <= ch <= "\u9fff" for ch in text)


def _is_raw_english(text: str) -> bool:
    stripped = text.replace("'", "").replace("-", "").replace(" ", "")
    if not stripped:
        return False
    if len(stripped) <= 1:
        return False
    return stripped.isascii() and stripped.isalpha() and any(ch.islower() for ch in stripped)


def convert_entry(entry: str) -> Iterator[str]:
    """Convert a single vocab entry into tokens."""
    if entry.startswith("<|unused"):
        yield entry
        return

    if _contains_cjk(entry):
        converted = lazy_pinyin(
            entry,
            style=Style.TONE3,
            neutral_tone_with_five=True,
            strict=False,
        )
        for item in converted:
            yield item
        return

    if _is_raw_english(entry):
        phonemes = phonemize(
            entry,
            language="en-us",
            strip=True,
            preserve_punctuation=True,
            with_stress=True,
        )
        phonemes = phonemes.replace(" ", "")
        for ch in phonemes:
            if ch:
                yield ch
        return

    yield entry


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Convert Matcha vocab list into tokens.txt format. "
            "将 Matcha 词表转换为 tokens.txt 映射。"
        )
    )
    parser.add_argument(
        "--vocab",
        type=Path,
        required=True,
        help="Path to vocab_tts.txt from the Matcha release. 指定 vocab_tts.txt 路径。",
    )
    parser.add_argument(
        "--output",
        type=Path,
        required=True,
        help="Output path for tokens.txt. 指定输出 tokens.txt 文件位置。",
    )
    return parser.parse_args()


def load_vocab(vocab_path: Path) -> list[str]:
    """Load vocab entries line by line (keeps blanks). 按行读取词表。"""
    entries: list[str] = []
    with vocab_path.open("r", encoding="utf-8") as infile:
        for raw in infile:
            token = raw.rstrip("\r\n")
            entries.append(token)
    return entries


def build_tokens(entries: Iterable[str]) -> list[str]:
    ordered: list[str] = []
    seen: set[str] = set()
    for entry in entries:
        for token in convert_entry(entry):
            if token not in seen:
                seen.add(token)
                ordered.append(token)
    return ordered


def write_tokens(tokens: list[str], output_path: Path) -> None:
    """Write `token id` pairs preserving order. 按原始顺序写出 token-id 对。"""
    output_path.parent.mkdir(parents=True, exist_ok=True)
    with output_path.open("w", encoding="utf-8") as outfile:
        for idx, token in enumerate(tokens):
            outfile.write(f"{token} {idx}\n")


def main() -> None:
    args = parse_args()
    entries = load_vocab(args.vocab)
    tokens = build_tokens(entries)
    write_tokens(tokens, args.output)


if __name__ == "__main__":
    main()
