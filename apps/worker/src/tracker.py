from typing import List, Dict, Any

def smooth_tracking_trajectory(detections: List[Dict[str, Any]], alpha: float = 0.25) -> List[Dict[str, Any]]:
    """
    Applies Exponential Moving Average (EMA) smoothing to subject centers.
    Prevents camera jerkiness during vertical 9:16 cropping.
    alpha: smoothing factor (lower = smoother camera movement, higher = faster response).
    """
    if not detections:
        return [{"x": 0.5, "y": 0.5, "width": 0.35, "height": 0.70}]

    smoothed = []
    prev_x = detections[0]["x"]
    prev_y = detections[0]["y"]
    prev_w = detections[0]["width"]
    prev_h = detections[0]["height"]

    for det in detections:
        # EMA calculation
        cur_x = alpha * det["x"] + (1 - alpha) * prev_x
        cur_y = alpha * det["y"] + (1 - alpha) * prev_y
        cur_w = alpha * det["width"] + (1 - alpha) * prev_w
        cur_h = alpha * det["height"] + (1 - alpha) * prev_h

        smoothed.append({
            "timestamp": det.get("timestamp", 0.0),
            "x": round(cur_x, 4),
            "y": round(cur_y, 4),
            "width": round(cur_w, 4),
            "height": round(cur_h, 4),
            "confidence": det.get("confidence", 0.8)
        })

        prev_x, prev_y, prev_w, prev_h = cur_x, cur_y, cur_w, cur_h

    return smoothed

def get_dominant_subject_center(smoothed_track: List[Dict[str, Any]]) -> float:
    """Computes the overall average horizontal center (0.0 - 1.0) of the main subject."""
    if not smoothed_track:
        return 0.5
    valid = [t["x"] for t in smoothed_track if t.get("confidence", 0) > 0.3]
    if not valid:
        return 0.5
    return sum(valid) / len(valid)
