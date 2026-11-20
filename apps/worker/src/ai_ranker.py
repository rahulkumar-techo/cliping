import os
import json
import time
import urllib.request
import urllib.error
from typing import List, Dict, Any

def rank_candidates_with_gemini(
    candidates: List[Dict[str, Any]],
    top_n: int = 3,
    api_key: str = "",
    model_name: str = "gemini-2.5-flash",
    max_retries: int = 3
) -> List[Dict[str, Any]]:
    """
    Ranks shortlisted candidates using Gemini API for semantic virality/retention evaluation.
    Only sends lightweight text transcript and timestamps (no full video upload).
    Falls back gracefully to local_score if Gemini is unavailable, 429, or invalid.
    """
    if not candidates:
        return []

    # If API key is missing, immediately fallback to local scores
    api_key = api_key or os.environ.get("GEMINI_API_KEY", "")
    if not api_key:
        print("[AI-RANKER] No GEMINI_API_KEY provided; falling back to local scores.")
        return _fallback_to_local_ranking(candidates, top_n)

    model_name = model_name or os.environ.get("GEMINI_MODEL", "gemini-2.5-flash")

    # Prepare compact prompt
    candidates_payload = []
    for c in candidates[:15]: # Max 15 candidates sent to Gemini
        candidates_payload.append({
            "index": c["candidate_index"],
            "start": c["start"],
            "end": c["end"],
            "duration": c["duration"],
            "transcript": c["transcript"]
        })

    prompt = f"""You are an elite short-form video editor for YouTube Shorts and Instagram Reels.
Evaluate the following candidate video clips for virality, hook strength, retention, and complete payoff.

Candidates:
{json.dumps(candidates_payload, indent=2)}

Return a strict JSON array ONLY (no markdown code blocks, no additional explanation) where each item has:
- "index": integer matching candidate index
- "gemini_score": integer from 0 to 100
- "reason": concise explanation of hook, payoff, and virality potential

Example format:
[
  {{"index": 0, "gemini_score": 92, "reason": "Strong hook with complete payoff and natural ending"}}
]
"""

    gemini_scores = {}
    url = f"https://generativelanguage.googleapis.com/v1beta/models/{model_name}:generateContent?key={api_key}"
    payload = {
        "contents": [{
            "parts": [{"text": prompt}]
        }],
        "generationConfig": {
            "temperature": 0.2,
            "responseMimeType": "application/json"
        }
    }

    # Retry loop with exponential backoff
    for attempt in range(max_retries):
        try:
            req = urllib.request.Request(
                url,
                data=json.dumps(payload).encode("utf-8"),
                headers={"Content-Type": "application/json"},
                method="POST"
            )
            with urllib.request.urlopen(req, timeout=15) as resp:
                data = json.loads(resp.read().decode("utf-8"))
                text_response = data["candidates"][0]["content"]["parts"][0]["text"]

                # Parse JSON array from response
                parsed = json.loads(text_response)
                if isinstance(parsed, list):
                    for item in parsed:
                        idx = item.get("index")
                        score = float(item.get("gemini_score", 70.0))
                        reason = item.get("reason", "Selected by Gemini")
                        gemini_scores[idx] = (score, reason)
                    break
        except urllib.error.HTTPError as e:
            print(f"[AI-RANKER] Gemini API HTTP {e.code} on attempt {attempt+1}/{max_retries}")
            if e.code in (429, 500, 503):
                time.sleep(2 ** attempt)
            else:
                break
        except Exception as e:
            print(f"[AI-RANKER] Error querying Gemini: {e}")
            time.sleep(1)

    # Compute final combined scores
    ranked = []
    for c in candidates:
        idx = c["candidate_index"]
        if idx in gemini_scores:
            g_score, reason = gemini_scores[idx]
        else:
            g_score = c["local_score"]
            reason = c.get("reason", "Ranked using local heuristics")

        # final_score = local_score * 0.55 + gemini_score * 0.45
        final_score = round((c["local_score"] * 0.55) + (g_score * 0.45), 2)
        c_copy = dict(c)
        c_copy["gemini_score"] = g_score
        c_copy["final_score"] = final_score
        c_copy["reason"] = reason
        ranked.append(c_copy)

    ranked.sort(key=lambda x: x["final_score"], reverse=True)
    return ranked[:top_n]

def _fallback_to_local_ranking(candidates: List[Dict[str, Any]], top_n: int) -> List[Dict[str, Any]]:
    ranked = []
    for c in candidates:
        c_copy = dict(c)
        c_copy["gemini_score"] = c["local_score"]
        c_copy["final_score"] = c["local_score"]
        ranked.append(c_copy)
    ranked.sort(key=lambda x: x["final_score"], reverse=True)
    return ranked[:top_n]
