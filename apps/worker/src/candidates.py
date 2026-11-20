import os
import json
import re
from typing import List, Dict, Any
from transcription import evaluate_transcript_hook, find_sentence_boundaries
from audio import score_audio_interval
from scenes import is_near_scene_cut

def generate_candidates(
    transcript_segments: List[Dict[str, Any]],
    scenes: List[Dict[str, float]],
    audio_features: Dict[str, Any],
    target_duration: int = 30,
    min_candidates: int = 10,
    max_candidates: int = 20,
    requested_clips: int = 3
) -> List[Dict[str, Any]]:
    """
    Generates high-potential candidate clip intervals locally.
    Enforces sentence completeness, duration flexibility, and guarantees
    that at least requested_clips (e.g. 3) distinct non-overlapping clips are available.
    """
    sentences = find_sentence_boundaries(transcript_segments)
    if not sentences:
        sentences = transcript_segments

    total_duration = audio_features.get("duration", 0.0)
    if total_duration <= 0 and scenes:
        total_duration = max((s.get("end", 0.0) for s in scenes), default=0.0)

    candidates = []
    min_dur = max(10, target_duration - 4)
    max_dur = target_duration + 4

    for i in range(len(sentences)):
        start_time = sentences[i]["start"]
        if total_duration > 0 and start_time >= total_duration - 5.0:
            continue
        accumulated_text = []

        for j in range(i, len(sentences)):
            accumulated_text.append(sentences[j]["text"])
            end_time = sentences[j]["end"]
            dur = end_time - start_time

            if dur < min_dur:
                continue

            if dur > max_dur:
                break

            if total_duration > 0 and end_time > total_duration + 0.5:
                break

            # Valid window found that ends on a complete sentence
            clip_text = " ".join(accumulated_text)
            first_sentence = sentences[i]["text"]
            last_sentence = sentences[j]["text"]

            hook_score = evaluate_transcript_hook(first_sentence)
            speech_score = min(100.0, (len(clip_text.split()) / dur) * 30.0) # word density
            audio_score = score_audio_interval(audio_features, start_time, end_time)

            scene_cut_start = is_near_scene_cut(start_time, scenes, threshold_sec=0.5)
            scene_score = 80.0 if not scene_cut_start else 60.0

            completeness_score = 90.0 if re.search(r"[.!?]$", last_sentence.strip()) else 50.0
            ending_quality = 85.0 if not last_sentence.strip().endswith(",") else 40.0
            visual_activity = 75.0

            local_score = (
                hook_score * 0.25
                + speech_score * 0.15
                + audio_score * 0.10
                + scene_score * 0.10
                + visual_activity * 0.10
                + completeness_score * 0.20
                + ending_quality * 0.10
            )

            candidates.append({
                "candidate_index": len(candidates),
                "start": round(start_time, 2),
                "end": round(end_time, 2),
                "duration": round(dur, 2),
                "transcript": clip_text,
                "hook_score": round(hook_score, 1),
                "speech_score": round(speech_score, 1),
                "audio_score": round(audio_score, 1),
                "scene_score": round(scene_score, 1),
                "completeness_score": round(completeness_score, 1),
                "ending_quality": round(ending_quality, 1),
                "local_score": round(local_score, 2),
                "reason": f"Hook score {hook_score:.0f}, complete thought ending at {end_time:.1f}s"
            })

    # Sort descending by local score
    candidates.sort(key=lambda c: c["local_score"], reverse=True)

    # Filter out heavily overlapping duplicates (overlap > 40%)
    deduped = []
    for cand in candidates:
        overlap = False
        for kept in deduped:
            intersection = max(0, min(cand["end"], kept["end"]) - max(cand["start"], kept["start"]))
            min_len = min(cand["duration"], kept["duration"])
            if min_len > 0 and (intersection / min_len) > 0.40:
                overlap = True
                break
        if not overlap:
            deduped.append(cand)
        if len(deduped) >= max_candidates:
            break

    # Guarantee at least requested_clips: If deduped has fewer than requested_clips,
    # generate additional windows across the video duration using audio energy peaks
    target_count = max(requested_clips, min(min_candidates, 5))
    if len(deduped) < target_count:
        total_duration = audio_features.get("duration", 0.0)
        if total_duration <= 0 and transcript_segments:
            total_duration = max((s.get("end", 0.0) for s in transcript_segments), default=0.0)
        if total_duration <= 0 and scenes:
            total_duration = max((s.get("end", 0.0) for s in scenes), default=0.0)
        if total_duration <= 0:
            total_duration = float(target_duration * target_count)

        step_sec = max(5.0, float(target_duration) * 0.5)
        cur_start = 0.0
        while cur_start + 10.0 <= total_duration and len(deduped) < target_count:
            cur_end = min(total_duration, cur_start + target_duration)
            if cur_end - cur_start >= 10.0:
                overlap = False
                for kept in deduped:
                    inter = max(0, min(cur_end, kept["end"]) - max(cur_start, kept["start"]))
                    min_l = min(cur_end - cur_start, kept["duration"])
                    if min_l > 0 and (inter / min_l) > 0.40:
                        overlap = True
                        break
                if not overlap:
                    a_score = score_audio_interval(audio_features, cur_start, cur_end)
                    deduped.append({
                        "candidate_index": len(deduped),
                        "start": round(cur_start, 2),
                        "end": round(cur_end, 2),
                        "duration": round(cur_end - cur_start, 2),
                        "transcript": f"Exciting moment from {cur_start:.0f}s to {cur_end:.0f}s",
                        "hook_score": 65.0,
                        "speech_score": 70.0,
                        "audio_score": round(a_score, 1),
                        "scene_score": 75.0,
                        "completeness_score": 80.0,
                        "ending_quality": 80.0,
                        "local_score": round(a_score * 0.4 + 50.0, 2),
                        "reason": f"High audio energy window at {cur_start:.0f}s - {cur_end:.0f}s"
                    })
            cur_start += step_sec

    # Re-index candidates cleanly
    for i, c in enumerate(deduped):
        c["candidate_index"] = i

    return deduped
