import os
import sys
import json
import gc
from typing import Dict, Any

from downloader import validate_youtube_url, download_youtube_video, check_disk_space_gb
from audio import extract_audio_16k_mono, analyze_audio_features
from transcription import transcribe_audio
from scenes import create_analysis_proxy, detect_scenes
from candidates import generate_candidates
from ai_ranker import rank_candidates_with_gemini
from detector import detect_subjects_in_clip
from tracker import smooth_tracking_trajectory, get_dominant_subject_center
from reframer import compute_9_16_crop_window, get_video_resolution, build_dynamic_pan_crop_filter
from captions import generate_ass_subtitles
from renderer import render_final_clip
from publisher import publish_to_youtube_shorts, publish_to_instagram_reels
from cleanup import cleanup_intermediate_files

def emit_event(event_type: str, **kwargs):
    """Prints structured JSON event line to stdout for Go orchestrator."""
    payload = {"event": event_type, **kwargs}
    sys.stdout.write(json.dumps(payload) + "\n")
    sys.stdout.flush()

def emit_stage(stage: str, progress: int):
    emit_event("stage", stage=stage, progress=progress)

def emit_log(stage: str, level: str, message: str):
    emit_event("log", stage=stage, level=level, message=message)

def run_pipeline(job: Dict[str, Any], storage_path: str):
    """
    Executes the 17-stage video processing pipeline.
    Optimized strictly for 4 CPU cores, 8 GB RAM, and 32 GB disk.
    """
    job_id = job["id"]
    job_dir = os.path.join(storage_path, job_id)
    final_dir = os.path.join(job_dir, "final")
    os.makedirs(final_dir, exist_ok=True)

    source_path = os.path.join(job_dir, "source.mp4")
    proxy_path = os.path.join(job_dir, "proxy.mp4")
    audio_path = os.path.join(job_dir, "audio.wav")
    transcript_path = os.path.join(job_dir, "transcript.json")
    scenes_path = os.path.join(job_dir, "scenes.json")
    candidates_path = os.path.join(job_dir, "candidates.json")

    youtube_url = job["youtube_url"]
    clip_duration = job.get("clip_duration", 30)
    clip_count = job.get("clip_count", 3)
    smart_tracking = bool(job.get("smart_tracking", True))
    auto_captions = bool(job.get("auto_captions", True))
    ai_ranking = bool(job.get("ai_ranking", True))
    auto_publish = job.get("auto_publish", "")

    # STAGE 1: Validating URL
    emit_stage("Validating URL", 5)
    emit_log("Validating URL", "INFO", f"Validating YouTube URL: {youtube_url}")
    if not validate_youtube_url(youtube_url):
        raise ValueError("Invalid YouTube URL")

    # Check available disk
    free_gb = check_disk_space_gb(job_dir)
    emit_log("Validating URL", "INFO", f"Free disk space available: {free_gb:.1f} GB")
    if free_gb < 8.0:
        raise RuntimeError(f"Low disk space warning: {free_gb:.1f} GB free, safety threshold is 8.0 GB")

    # STAGE 2: Downloading
    emit_stage("Downloading", 12)
    emit_log("Downloading", "INFO", "Downloading video stream (up to 1080p)...")
    download_youtube_video(youtube_url, source_path)
    emit_log("Downloading", "INFO", f"Downloaded source video ({os.path.getsize(source_path) / (1024*1024):.1f} MB)")

    # STAGE 3: Extracting audio
    emit_stage("Extracting audio", 20)
    emit_log("Extracting audio", "INFO", "Extracting 16kHz mono PCM audio for Whisper...")
    extract_audio_16k_mono(source_path, audio_path)

    # STAGE 4: Creating analysis proxy
    emit_stage("Creating analysis proxy", 28)
    emit_log("Creating analysis proxy", "INFO", "Generating fast 640x360 keyframe analysis proxy...")
    create_analysis_proxy(source_path, proxy_path)
    proxy_sz = os.path.getsize(proxy_path) if os.path.exists(proxy_path) else 0
    emit_log("Creating analysis proxy", "INFO", f"Analysis proxy generated ({proxy_sz / (1024*1024):.1f} MB).")

    # STAGE 5: Transcribing (Whisper)
    emit_stage("Transcribing", 38)
    whisper_model = os.environ.get("WHISPER_MODEL", "base")
    emit_log("Transcribing", "INFO", f"Transcribing with faster-whisper ({whisper_model})...")
    segments = transcribe_audio(audio_path, transcript_path, model_size=whisper_model)
    emit_log("Transcribing", "INFO", f"Transcription complete. Extracted {len(segments)} speech segments. Whisper memory freed.")

    # STAGE 6: Detecting scenes
    emit_stage("Detecting scenes", 46)
    emit_log("Detecting scenes", "INFO", "Detecting visual scene cut boundaries...")
    scenes = detect_scenes(proxy_path, scenes_path)

    # STAGE 7: Analyzing audio
    emit_stage("Analyzing audio", 52)
    emit_log("Analyzing audio", "INFO", "Computing RMS energy, peaks, and silence timeline...")
    audio_features = analyze_audio_features(audio_path)

    # STAGE 8: Generating candidates
    emit_stage("Generating candidates", 58)
    emit_log("Generating candidates", "INFO", f"Evaluating sentence windows around {clip_duration}s (target: {clip_count} clips)...")
    candidates = generate_candidates(
        segments,
        scenes,
        audio_features,
        target_duration=clip_duration,
        requested_clips=clip_count
    )
    with open(candidates_path, "w", encoding="utf-8") as f:
        json.dump(candidates, f, indent=2)
    emit_log("Generating candidates", "INFO", f"Generated {len(candidates)} high-potential candidates with local heuristic scoring.")

    # STAGE 9: AI ranking (Gemini)
    emit_stage("AI ranking", 66)
    if ai_ranking:
        gemini_model = os.environ.get("GEMINI_MODEL", "gemini-2.5-flash")
        emit_log("AI ranking", "INFO", f"Querying Gemini ({gemini_model}) for semantic payoff ranking...")
        ranked_clips = rank_candidates_with_gemini(candidates, top_n=clip_count, model_name=gemini_model)
    else:
        emit_log("AI ranking", "INFO", "AI Ranking disabled; selecting top candidates via local score.")
        ranked_clips = sorted(candidates, key=lambda c: c["local_score"], reverse=True)[:clip_count]

    # Guarantee ranked_clips contains requested clip_count
    if len(ranked_clips) < clip_count:
        for c in candidates:
            if c not in ranked_clips:
                ranked_clips.append(c)
            if len(ranked_clips) >= clip_count:
                break

    # STAGES 10, 11, 12, 13, 14: Per-Clip Tracking, Reframing, Subtitling, Rendering
    source_w, source_h = get_video_resolution(source_path)
    output_clips_records = []

    for idx, clip in enumerate(ranked_clips, start=1):
        clip_id = f"{job_id}_clip_{idx}"
        output_mp4 = os.path.join(final_dir, f"clip_{idx}.mp4")
        ass_path = os.path.join(job_dir, f"subtitles_{idx}.ass")
        start_t = clip["start"]
        end_t = clip["end"]

        crop_filter_override = None
        duration = round(end_t - start_t, 2)

        # STAGE 10 & 11: Detecting subjects & Tracking (YOLO only for shortlisted clips!)
        emit_stage("Detecting subjects", 70 + (idx * 3))
        if smart_tracking:
            yolo_model = os.environ.get("YOLO_MODEL", "yolov8n.pt")
            emit_log("Detecting subjects", "INFO", f"[Clip #{idx}] Loading YOLO ({yolo_model}) for subject detection...")
            detections = detect_subjects_in_clip(proxy_path, start_t, end_t, model_name=yolo_model)
            emit_stage("Tracking subject", 72 + (idx * 3))
            smoothed = smooth_tracking_trajectory(detections)
            subj_center_x = get_dominant_subject_center(smoothed)
            emit_log("Tracking subject", "INFO", f"[Clip #{idx}] Dominant subject centered at x={subj_center_x:.2f}")

            # STAGE 12: Smart reframing with dynamic camera tracking & safe-margin padding
            emit_stage("Smart reframing", 78 + idx)
            crop_filter_override, crop_x, crop_y, crop_w, crop_h = build_dynamic_pan_crop_filter(
                source_w, source_h, smoothed, duration, target_w=1080, target_h=1920
            )
            emit_log("Smart reframing", "INFO", f"[Clip #{idx}] Camera focus locked on subject to prevent clipping at mobile 9:16 borders.")
        else:
            subj_center_x = 0.5
            emit_stage("Smart reframing", 78 + idx)
            crop_x, crop_y, crop_w, crop_h = compute_9_16_crop_window(source_w, source_h, subject_center_x=subj_center_x)

        # STAGE 13: Generating captions (Optional Toggle)
        emit_stage("Generating captions", 82 + idx)
        sub_file = None
        if auto_captions:
            emit_log("Generating captions", "INFO", f"[Clip #{idx}] Formatting mobile safe-margin ASS captions...")
            sub_file = generate_ass_subtitles(segments, start_t, end_t, ass_path)
        else:
            emit_log("Generating captions", "INFO", f"[Clip #{idx}] Captions toggled OFF by user. Generating clean video.")

        # STAGE 14: Rendering
        emit_stage("Rendering", 85 + (idx * 2))
        emit_log("Rendering", "INFO", f"[Clip #{idx}] Rendering 1080x1920 H.264/AAC with FFmpeg...")
        render_final_clip(
            source_video=source_path,
            output_clip_path=output_mp4,
            start_sec=start_t,
            end_sec=end_t,
            crop_x=crop_x,
            crop_y=crop_y,
            crop_w=crop_w,
            crop_h=crop_h,
            ass_subtitles_path=sub_file,
            crop_filter_override=crop_filter_override
        )

        thumb_path = output_mp4.rsplit(".", 1)[0] + ".jpg"

        clip_rec = {
            "id": clip_id,
            "job_id": job_id,
            "clip_index": idx,
            "start_time": start_t,
            "end_time": end_t,
            "duration": round(end_t - start_t, 2),
            "local_score": clip.get("local_score", 0.0),
            "gemini_score": clip.get("gemini_score", 0.0),
            "final_score": clip.get("final_score", 0.0),
            "reason": clip.get("reason", ""),
            "file_path": output_mp4,
            "thumbnail_path": thumb_path if os.path.exists(thumb_path) else "",
            "upload_status": "pending"
        }
        output_clips_records.append(clip_rec)
        emit_event("clip", clip=clip_rec)

    # STAGE 15: Uploading (if auto_publish configured)
    emit_stage("Uploading", 94)
    if auto_publish:
        for clip_rec in output_clips_records:
            if "youtube" in auto_publish:
                emit_log("Uploading", "INFO", f"Auto-publishing Clip #{clip_rec['clip_index']} to YouTube Shorts...")
                yt_res = publish_to_youtube_shorts(clip_rec["file_path"])
                emit_log("Uploading", "INFO", f"YouTube Shorts published: {yt_res.get('external_id')}")
            if "instagram" in auto_publish:
                emit_log("Uploading", "INFO", f"Auto-publishing Clip #{clip_rec['clip_index']} to Instagram Reels...")
                ig_res = publish_to_instagram_reels(clip_rec["file_path"])
                emit_log("Uploading", "INFO", f"Instagram Reels published: {ig_res.get('external_id')}")
    else:
        emit_log("Uploading", "INFO", "Auto-publish not selected. Clips ready for local download/manual publish.")

    # STAGE 16: Cleanup
    emit_stage("Cleanup", 98)
    emit_log("Cleanup", "INFO", "Reclaiming temporary disk space (removing source, proxy, extracted audio)...")
    cleanup_intermediate_files(job_dir)
    emit_log("Cleanup", "INFO", "Cleanup completed. Final clips preserved in final/.")

    # STAGE 17: Completed
    emit_stage("Completed", 100)
    emit_event("completed", job_id=job_id, clips_count=len(output_clips_records))
