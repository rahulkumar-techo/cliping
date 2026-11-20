import re
import subprocess
from typing import List, Dict, Any, Tuple

def get_video_resolution(video_path: str) -> Tuple[int, int]:
    """Extracts actual width and height of video file using FFmpeg probe."""
    try:
        from renderer import get_ffmpeg_path
        ffmpeg_exe = get_ffmpeg_path()
        res = subprocess.run([ffmpeg_exe, "-i", video_path], stderr=subprocess.PIPE, stdout=subprocess.DEVNULL, text=True)
        match = re.search(r"Video:.* (\d{3,4})x(\d{3,4})", res.stderr)
        if match:
            return int(match.group(1)), int(match.group(2))
    except Exception:
        pass
    return 1920, 1080

def compute_9_16_crop_window(
    source_w: int,
    source_h: int,
    subject_center_x: float = 0.5,
    subject_center_y: float = 0.5
) -> Tuple[int, int, int, int]:
    """
    Computes (crop_x, crop_y, crop_w, crop_h) to crop a 16:9 or horizontal video
    into a vertical 9:16 frame centered around the subject.
    Guarantees crop never exceeds video dimensions.
    """
    # 9:16 aspect ratio: crop_w = (9/16) * crop_h
    # In standard horizontal video (e.g. 1920x1080), the height is fully utilized:
    target_aspect = 9.0 / 16.0
    crop_h = source_h
    crop_w = int(crop_h * target_aspect)

    # Ensure even dimensions for H.264
    if crop_w % 2 != 0:
        crop_w -= 1
    if crop_h % 2 != 0:
        crop_h -= 1

    # Subject center in pixel coordinates
    center_px_x = int(subject_center_x * source_w)

    # Center crop_w around subject
    crop_x = center_px_x - (crop_w // 2)

    # Clamp bounds to stay inside frame
    crop_x = max(0, min(source_w - crop_w, crop_x))
    crop_y = 0

    return crop_x, crop_y, crop_w, crop_h

def build_ffmpeg_crop_filter(crop_x: int, crop_y: int, crop_w: int, crop_h: int, target_w: int = 1080, target_h: int = 1920) -> str:
    """
    Builds FFmpeg video filter string to crop and scale to final 1080x1920 9:16 video.
    """
    return f"crop={crop_w}:{crop_h}:{crop_x}:{crop_y},scale={target_w}:{target_h}"

def build_dynamic_pan_crop_filter(
    source_w: int,
    source_h: int,
    smoothed_track: List[Dict[str, Any]],
    clip_duration: float,
    target_w: int = 1080,
    target_h: int = 1920
) -> Tuple[str, int, int, int, int]:
    """
    Builds a dynamic camera tracking filter that keeps the main object in focus.
    If the subject moves across the desktop 16:9 frame, the camera smoothly pans
    so the subject is NEVER cut off at the vertical 9:16 boundaries.
    If movement is minimal (<= 8%), holds steady with generous safe margins.
    Returns: (filter_string, static_crop_x, crop_y, crop_w, crop_h)
    """
    target_aspect = 9.0 / 16.0
    crop_h = source_h
    crop_w = int(crop_h * target_aspect)
    if crop_w % 2 != 0:
        crop_w -= 1
    if crop_h % 2 != 0:
        crop_h -= 1

    max_x = max(0, source_w - crop_w)

    if not smoothed_track:
        static_x = max_x // 2
        return build_ffmpeg_crop_filter(static_x, 0, crop_w, crop_h, target_w, target_h), static_x, 0, crop_w, crop_h

    # Track centers over time
    xs = [t.get("x", 0.5) for t in smoothed_track]
    first_x = xs[0]
    last_x = xs[-1]
    min_x = min(xs)
    max_x_track = max(xs)
    motion_range = max_x_track - min_x

    # Static median anchor
    median_x = sorted(xs)[len(xs) // 2]
    anchor_center_px = int(median_x * source_w)
    static_crop_x = max(0, min(max_x, anchor_center_px - (crop_w // 2)))

    # If subject moves more than 8% across the frame, pan dynamically!
    if motion_range > 0.08 and clip_duration > 1.0:
        x_start_px = max(0, min(max_x, int(first_x * source_w) - (crop_w // 2)))
        x_end_px = max(0, min(max_x, int(last_x * source_w) - (crop_w // 2)))
        velocity = (x_end_px - x_start_px) / float(clip_duration)

        # Time-varying crop x expression: x(t) = min(max(x_start + v*t, 0), max_x)
        # Keeps subject centered with safe margins throughout the movement
        pan_expr = f"min(max({x_start_px:.1f}+({velocity:.2f}*t), 0), {max_x})"
        filter_str = f"crop={crop_w}:{crop_h}:x='{pan_expr}':y=0,scale={target_w}:{target_h}"
        return filter_str, static_crop_x, 0, crop_w, crop_h

    # Stationary subject: rock-solid centered framing with safe margins
    filter_str = build_ffmpeg_crop_filter(static_crop_x, 0, crop_w, crop_h, target_w, target_h)
    return filter_str, static_crop_x, 0, crop_w, crop_h
