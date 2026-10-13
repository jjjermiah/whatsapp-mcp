import pytest

import whatsapp
from markdown_format import format_outgoing, markdown_to_whatsapp


@pytest.mark.parametrize(
    ("source", "expected"),
    [
        # bold / strike
        ("**bold**", "*bold*"),
        ("__bold__", "*bold*"),
        ("a **bold** and **more** text", "a *bold* and *more* text"),
        ("**bold with spaces inside**", "*bold with spaces inside*"),
        ("~~gone~~", "~gone~"),
        # headings
        ("# Title", "*Title*"),
        ("### Deep heading ###", "*Deep heading*"),
        ("# **Already bold**", "*Already bold*"),
        ("intro\n## Section\nbody", "intro\n*Section*\nbody"),
        ("# C#", "*C#*"),
        # links
        ("[docs](https://example.com/a?b=1)", "docs (https://example.com/a?b=1)"),
        ("[https://example.com](https://example.com)", "https://example.com"),
        ("[**bold link**](https://x.io)", "*bold link* (https://x.io)"),
        # already WhatsApp syntax: untouched
        ("*bold* and _italic_ and ~strike~", "*bold* and _italic_ and ~strike~"),
        ("plain text, no markup", "plain text, no markup"),
        ("5 * 3 * 2 = 30", "5 * 3 * 2 = 30"),
        ("- item one\n- item two", "- item one\n- item two"),
        ("#hashtag and # alone", "#hashtag and # alone"),
        # not markers: arithmetic, snake_case, unmatched
        ("2**3 and 4**5", "2**3 and 4**5"),
        ("some__snake__case", "some__snake__case"),
        ("**unclosed", "**unclosed"),
        ("** spaced **", "** spaced **"),
        ("", ""),
    ],
)
def test_markdown_to_whatsapp(source, expected):
    assert markdown_to_whatsapp(source) == expected


@pytest.mark.parametrize(
    "code",
    [
        "`**not bold**`",
        "```\n**not bold**\n# not a heading\n[a](b)\n```",
        "```python\nx = a**2 + b**2\n```",
    ],
)
def test_code_is_left_untouched(code):
    assert markdown_to_whatsapp(code) == code


def test_code_untouched_but_surrounding_text_converted():
    source = "**Run** `foo **bar**` then:\n```\n# comment\n```\n**Done**"
    assert markdown_to_whatsapp(source) == "*Run* `foo **bar**` then:\n```\n# comment\n```\n*Done*"


def test_realistic_invite():
    source = (
        "# Wedding **invite**\n\nHi **Sam**! Details: [RSVP here](https://rsvp.example.com).\n~~Old date~~ -> June 5"
    )
    assert markdown_to_whatsapp(source) == (
        "*Wedding invite*\n\nHi *Sam*! Details: RSVP here (https://rsvp.example.com).\n~Old date~ -> June 5"
    )


@pytest.mark.parametrize("value", ["0", "false", "FALSE", "no", "off", " 0 "])
def test_format_outgoing_can_be_disabled(monkeypatch, value):
    monkeypatch.setenv("WHATSAPP_MARKDOWN_CONVERT", value)
    assert format_outgoing("**x**") == "**x**"


@pytest.mark.parametrize("value", [None, "1", "true", ""])
def test_format_outgoing_default_on(monkeypatch, value):
    if value is None:
        monkeypatch.delenv("WHATSAPP_MARKDOWN_CONVERT", raising=False)
    else:
        monkeypatch.setenv("WHATSAPP_MARKDOWN_CONVERT", value)
    assert format_outgoing("**x**") == "*x*"


class _Resp:
    status_code = 200
    text = ""

    def json(self):
        return {"success": True, "message": "sent"}


def test_send_message_and_send_file_caption_are_converted(monkeypatch, tmp_path):
    posted = []

    def fake_post(url, json, headers=None):
        posted.append(json)
        return _Resp()

    monkeypatch.delenv("WHATSAPP_MARKDOWN_CONVERT", raising=False)
    monkeypatch.setattr(whatsapp.requests, "post", fake_post)
    media = tmp_path / "a.jpg"
    media.write_bytes(b"x")

    whatsapp.send_message("12025551234", "**hi** ~~there~~")
    whatsapp.send_file("12025551234", str(media), "# Cap **x**")

    assert posted[0]["message"] == "*hi* ~there~"
    assert posted[1]["message"] == "*Cap x*"


def test_send_message_not_converted_when_disabled(monkeypatch):
    posted = []
    monkeypatch.setenv("WHATSAPP_MARKDOWN_CONVERT", "0")
    monkeypatch.setattr(whatsapp.requests, "post", lambda url, json, headers=None: posted.append(json) or _Resp())

    whatsapp.send_message("12025551234", "**hi**")

    assert posted[0]["message"] == "**hi**"
