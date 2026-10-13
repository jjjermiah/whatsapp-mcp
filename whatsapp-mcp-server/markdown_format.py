"""Convert common Markdown to WhatsApp's own text formatting.

WhatsApp renders *bold*, _italic_, ~strike~, `inline mono` and ```mono blocks```.
Agents tend to write Markdown, so `**bold**` would arrive with literal asterisks.
Single *x* and _x_ are already WhatsApp syntax and are left alone, as is anything
inside code spans/fences and URLs. `__x__` is not converted: it is far more often
an identifier (`__init__`, `__pycache__`) than Markdown bold.
"""

import os
import re

_CODE = re.compile(r"```.*?```|`[^`\n]+`", re.DOTALL)
_PLACEHOLDER = re.compile(r"\x00(\d+)\x00")
_LINK = re.compile(r"\[([^\]\n]+)\]\(([^)\s]+)\)")
_URL = re.compile(r"https?://\S+")
# Markdown closing a URL (`**https://x.com**`, `(see https://x.com).`) is not part of it.
_URL_TRAILING = "*~.,;:!?)]"
# Bold/strike markers must not touch a word character on the outside, so `2**3 and 4**5`
# survives.
_BOLD_ITALIC_STARS = re.compile(r"(?<![\w*])\*\*\*(?=\S)(.+?)(?<=\S)\*\*\*(?![\w*])")
_BOLD_STARS = re.compile(r"(?<![\w*])\*\*(?=\S)(.+?)(?<=\S)\*\*(?![\w*])")
_STRIKE = re.compile(r"(?<![\w~])~~(?=\S)(.+?)(?<=\S)~~(?![\w~])")
_HEADING = re.compile(r"^[ \t]{0,3}#{1,6}[ \t]+(.+?)(?:[ \t]+#+)?[ \t]*$", re.MULTILINE)


def _heading(match: re.Match[str]) -> str:
    # A heading is bold as a whole, so drop inner `**` markers instead of nesting them.
    # Lone `*` (arithmetic, footnotes) is content and stays.
    text = match.group(1).replace("**", "").strip()
    return f"*{text}*" if text else match.group(0)


def _link(match: re.Match[str]) -> str:
    label, url = match.group(1), match.group(2)
    return url if label == url else f"{label} ({url})"


def markdown_to_whatsapp(text: str) -> str:
    """Rewrite Markdown bold, strike, headings and links into WhatsApp syntax."""
    spans: list[str] = []

    def stash_text(span: str) -> str:
        spans.append(span)
        return f"\x00{len(spans) - 1}\x00"

    def stash(match: re.Match[str]) -> str:
        return stash_text(match.group(0))

    def stash_url(match: re.Match[str]) -> str:
        url = match.group(0).rstrip(_URL_TRAILING)
        return stash_text(url) + match.group(0)[len(url) :]

    def restore(match: re.Match[str]) -> str:
        return _PLACEHOLDER.sub(restore, spans[int(match.group(1))])

    out = _CODE.sub(stash, text)
    out = _LINK.sub(_link, out)
    # After links, so link targets (now bare URLs) are protected too.
    out = _URL.sub(stash_url, out)
    # Before bold so a `# **title**` heading is flattened rather than left as `*title*` inside `*...*`.
    out = _HEADING.sub(_heading, out)
    out = _BOLD_ITALIC_STARS.sub(r"*_\1_*", out)
    out = _BOLD_STARS.sub(r"*\1*", out)
    out = _STRIKE.sub(r"~\1~", out)
    return _PLACEHOLDER.sub(restore, out)


def format_outgoing(text: str) -> str:
    """Convert unless WHATSAPP_MARKDOWN_CONVERT is 0/false/no/off."""
    if os.getenv("WHATSAPP_MARKDOWN_CONVERT", "1").strip().lower() in {"0", "false", "no", "off"}:
        return text
    return markdown_to_whatsapp(text)
