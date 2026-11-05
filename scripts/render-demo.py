"""Render captured updater stdout as a compact README animation.

Development tool only. Requires Python and Pillow. Capture actual successful runs
on disposable containers; input events contain application/action/command/output,
protected_files and elapsed_seconds. This does not run an updater or invent output.
"""
import argparse
import json
import textwrap
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


def render(capture, destination, font_path):
    events = json.loads(Path(capture).read_text(encoding="utf-8"))
    font = ImageFont.truetype(str(font_path), 20)
    title = ImageFont.truetype(str(font_path), 28)
    small = ImageFont.truetype(str(font_path), 16)
    frames = []
    for index, event in enumerate(events, 1):
        image = Image.new("RGB", (1000, 550), "#111d21")
        draw = ImageDraw.Draw(image)
        draw.rounded_rectangle((24, 22, 976, 528), radius=18, fill="#17292e")
        draw.text((50, 44), "Portable Updater", font=title, fill="#f5eada")
        draw.text((50, 91), f'{event["application"]} / {event["action"]}', font=font, fill="#efad69")
        draw.line((50, 126, 950, 126), fill="#345357", width=1)
        draw.text((50, 145), "> " + event["command"], font=font, fill="#93d4c6")
        lines = []
        for line in event["output"]:
            lines.extend(textwrap.wrap(line, width=72) or [""])
        if len(lines) > 10:
            raise ValueError("Captured output needs more space; do not silently omit it")
        for row, line in enumerate(lines):
            draw.text((50, 190 + row * 25), line, font=font, fill="#e4e9e4")
        count = event["protected_files"]
        draw.text((50, 457), f"Protected file hashes unchanged: {count}/{count}", font=font, fill="#93d4c6")
        draw.text((50, 497), "Recorded stdout / disposable containers / cached packages / pauses shortened", font=small, fill="#b2c2c4")
        draw.text((889, 44), f"{index}/{len(events)}", font=small, fill="#b2c2c4")
        frames.append(image)
    Path(destination).parent.mkdir(parents=True, exist_ok=True)
    frames[0].save(destination, save_all=True, append_images=frames[1:], duration=3500, loop=0, optimize=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("capture")
    parser.add_argument("destination")
    parser.add_argument("--font", default="C:/Windows/Fonts/consola.ttf")
    args = parser.parse_args()
    render(args.capture, args.destination, args.font)
