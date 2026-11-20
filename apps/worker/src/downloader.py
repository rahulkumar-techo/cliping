import os
import shutil
import subprocess
import json
import re

def get_ffmpeg_path() -> str:
    """Finds FFmpeg executable: checks PATH, then imageio-ffmpeg."""
    which_ffmpeg = shutil.which("ffmpeg")
    if which_ffmpeg:
        return which_ffmpeg
    try:
        import imageio_ffmpeg
        return imageio_ffmpeg.get_ffmpeg_exe()
    except Exception:
        return "ffmpeg"

def check_disk_space_gb(path: str) -> float:
    """Returns free disk space in Gigabytes for the drive holding path."""
    try:
        check_dir = path if os.path.exists(path) else os.path.dirname(os.path.abspath(path))
        total, used, free = shutil.disk_usage(check_dir)
        return free / (1024 ** 3)
    except Exception:
        return 32.0

def validate_youtube_url(url: str) -> bool:
    """Validates that URL is a recognized YouTube video or Shorts link."""
    pattern = r"^(https?://)?(www\.)?(youtube\.com/watch\?v=|youtu\.be/|youtube\.com/shorts/)([a-zA-Z0-9_-]{11})"
    return bool(re.match(pattern, url.strip()))

def get_video_info(url: str) -> dict:
    """Fetches video metadata using yt-dlp Python library or CLI."""
    # Method 1: Direct Python yt_dlp library
    try:
        import yt_dlp
        ydl_opts = {
            "quiet": True,
            "no_warnings": True,
            "skip_download": True,
            "extract_flat": False,
        }
        with yt_dlp.YoutubeDL(ydl_opts) as ydl:
            return ydl.extract_info(url, download=False)
    except Exception:
        pass

    # Method 2: CLI subprocess
    cmd = ["yt-dlp", "--dump-json", "--no-playlist", "--skip-download", url]
    try:
        result = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=30)
        if result.returncode == 0:
            return json.loads(result.stdout)
    except Exception:
        pass

    return {"title": "YouTube Video", "duration": 120, "width": 1280, "height": 720}

def download_youtube_video(url: str, output_path: str, max_duration_mins: int = 120, min_disk_gb: float = 8.0) -> str:
    """
    Downloads YouTube video using yt-dlp Python API with FFmpeg merging.
    Optimized for low disk: downloads 720p/1080p stream to avoid 4K overhead.
    """
    if not validate_youtube_url(url):
        raise ValueError(f"Invalid YouTube URL format: {url}")

    dest_dir = os.path.dirname(os.path.abspath(output_path))
    os.makedirs(dest_dir, exist_ok=True)

    free_gb = check_disk_space_gb(dest_dir)
    if free_gb < min_disk_gb:
        raise RuntimeError(f"Insufficient disk space: {free_gb:.1f} GB free, minimum {min_disk_gb:.1f} GB required")

    info = get_video_info(url)
    duration = info.get("duration", 0)
    if duration and duration > max_duration_mins * 60:
        raise ValueError(f"Video duration ({duration // 60} mins) exceeds maximum allowed ({max_duration_mins} mins)")

    ffmpeg_exe = get_ffmpeg_path()

    # Prefer native Python yt_dlp API for maximum reliability
    try:
        import yt_dlp
        ydl_opts = {
            "format": "bestvideo[height<=1080][ext=mp4]+bestaudio[ext=m4a]/best[height<=1080]/best",
            "outtmpl": output_path.replace(".mp4", "") + ".%(ext)s",
            "merge_output_format": "mp4",
            "quiet": False,
            "no_warnings": False,
            "ffmpeg_location": ffmpeg_exe if os.path.exists(ffmpeg_exe) else None
        }
        with yt_dlp.YoutubeDL(ydl_opts) as ydl:
            ydl.download([url])

        if os.path.exists(output_path) and os.path.getsize(output_path) > 0:
            return output_path

        # If downloaded under slightly different name, find and rename to output_path
        base = output_path.replace(".mp4", "")
        for f in os.listdir(dest_dir):
            full_f = os.path.join(dest_dir, f)
            if full_f.startswith(base) and full_f.endswith((".mp4", ".mkv", ".webm")):
                if full_f != output_path:
                    shutil.move(full_f, output_path)
                return output_path
    except ImportError:
        pass
    except Exception as e:
        print(f"[DOWNLOADER] yt_dlp Python module error: {e}, attempting CLI fallback...")

    # CLI fallback
    format_spec = "bestvideo[height<=1080][ext=mp4]+bestaudio[ext=m4a]/best[height<=1080][ext=mp4]/best"
    cmd = [
        "yt-dlp",
        "-f", format_spec,
        "--merge-output-format", "mp4",
        "--no-playlist",
        "-o", output_path,
        url
    ]
    if os.path.exists(ffmpeg_exe):
        cmd.extend(["--ffmpeg-location", ffmpeg_exe])

    try:
        proc = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=600)
        if proc.returncode != 0 and not os.path.exists(output_path):
            # Fallback simple format
            cmd_fallback = ["yt-dlp", "-f", "best", "-o", output_path, url]
            if os.path.exists(ffmpeg_exe):
                cmd_fallback.extend(["--ffmpeg-location", ffmpeg_exe])
            proc_fb = subprocess.run(cmd_fallback, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=300)
            if proc_fb.returncode != 0 and not os.path.exists(output_path):
                raise RuntimeError(f"yt-dlp download failed: {proc.stderr[:300]}")
    except FileNotFoundError:
        if not os.path.exists(output_path):
            raise RuntimeError("yt-dlp is not installed. Run: pip install yt-dlp")

    if not os.path.exists(output_path) or os.path.getsize(output_path) == 0:
        raise RuntimeError("Downloaded video file is missing or empty")

    return output_path
