import os
from typing import List, Dict, Any

def format_timestamp_ass(seconds: float) -> str:
    """Formats float seconds into ASS timestamp: H:MM:SS.cs"""
    hrs = int(seconds // 3600)
    mins = int((seconds % 3600) // 60)
    secs = int(seconds % 60)
    centis = int(round((seconds - int(seconds)) * 100))
    if centis == 100:
        centis = 99
    return f"{hrs}:{mins:02d}:{secs:02d}.{centis:02d}"

def generate_ass_subtitles(
    segments: List[Dict[str, Any]],
    clip_start: float,
    clip_end: float,
    output_ass_path: str,
    font_size: int = 56,
    margin_v: int = 240
) -> str:
    """
    Generates an Advanced SubStation Alpha (.ass) subtitle file.
    Tailored for 9:16 mobile displays with bold yellow/white text and dark outlines.
    Placed in safe margin zone above bottom Reels/Shorts UI controls.
    """
    header = f"""[Script Info]
ScriptType: v4.00+
PlayResX: 1080
PlayResY: 1920
ScaledBorderAndShadow: yes

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Arial,{font_size},&H00FFFFFF,&H000000FF,&H00000000,&H80000000,-1,0,0,0,100,100,0,0,1,4,2,2,40,40,{margin_v},1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
"""
    events = []
    for s in segments:
        seg_start = s["start"]
        seg_end = s["end"]

        # Only include dialogue overlapping this clip
        if seg_end <= clip_start or seg_start >= clip_end:
            continue

        rel_start = max(0.0, seg_start - clip_start)
        rel_end = min(clip_end - clip_start, seg_end - clip_start)
        if rel_end <= rel_start:
            continue

        start_str = format_timestamp_ass(rel_start)
        end_str = format_timestamp_ass(rel_end)
        text = s.get("text", "").strip()

        # Clean punctuation for clean subtitle punch
        events.append(f"Dialogue: 0,{start_str},{end_str},Default,,0,0,0,,{text}")

    os.makedirs(os.path.dirname(os.path.abspath(output_ass_path)), exist_ok=True)
    with open(output_ass_path, "w", encoding="utf-8") as f:
        f.write(header)
        f.write("\n".join(events))
        f.write("\n")

    return output_ass_path
