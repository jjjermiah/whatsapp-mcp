"""Convert common Markdown to WhatsApp's own text formatting.

WhatsApp renders *bold*, _italic_, ~strike~, `inline mono` and ```mono blocks```.
Agents tend to write Markdown, so `**bold**` would arrive with literal asterisks.
Single *x* and _x_ are already WhatsApp syntax and are left alone, as is anything
inside code spans/fences.
"""

import os
import re

_CODE = re.compile(r"```.*?```|`[^`\n]+`", re.DOTALL)
_PLACEHOLDER = re.compile(r"\x00(\d+)\x00")
_LINK = re.compile(r"\[([^\]\n]+)\]\(([^)\s]+)\)")
# Bold/strike markers must not touch a word character on the outside, so `2**3 and 4**5`
# and `snake__case__name` survive.
_BOLD_STARS = re.compile(r"(?<![\w*])\*\*(?=\S)(.+?)(?<=\S)\*\*(?![\w*])")
_BOLD_UNDERSCORES = re.compile(r"(?<![\w_])__(?=\S)(.+?)(?<=\S)__(?![\w_])")
_STRIKE = re.compile(r"(?<![\w~])~~(?=\S)(.+?)(?<=\S)~~(?![\w~])")
_HEADING = re.compile(r"^[ \t]{0,3}#{1,6}[ \t]+(.+?)(?:[ \t]+#+)?[ \t]*$", re.MULTILINE)


def _heading(match: re.Match[str]) -> str:
    # A heading is bold as a whole, so drop inner bold markers instead of nesting them.
    text = match.group(1).replace("*", "").strip()
    return f"*{text}*" if text else match.group(0)


def _link(match: re.Match[str]) -> str:
    label, url = match.group(1), match.group(2)
    return url if label == url else f"{label} ({url})"


def markdown_to_whatsapp(text: str) -> str:
    """Rewrite Markdown bold, strike, headings and links into WhatsApp syntax."""
    spans: list[str] = []

    def stash(match: re.Match[str]) -> str:
        spans.append(match.group(0))
        return f"\x00{len(spans) - 1}\x00"

    out = _CODE.sub(stash, text)
    out = _LINK.sub(_link, out)
    out = _BOLD_STARS.sub(r"*\1*", out)
    out = _BOLD_UNDERSCORES.sub(r"*\1*", out)
    out = _STRIKE.sub(r"~\1~", out)
    out = _HEADING.sub(_heading, out)
    return _PLACEHOLDER.sub(lambda m: spans[int(m.group(1))], out)


def format_outgoing(text: str) -> str:
    """Convert unless WHATSAPP_MARKDOWN_CONVERT is 0/false/no/off."""
    if os.getenv("WHATSAPP_MARKDOWN_CONVERT", "1").strip().lower() in {"0", "false", "no", "off"}:
        return text
    return markdown_to_whatsapp(text)
