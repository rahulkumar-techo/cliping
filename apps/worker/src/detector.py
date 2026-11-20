import os
import gc
from typing import List, Dict, Any

def detect_subjects_in_clip(
    proxy_video: str,
    start_sec: float,
    end_sec: float,
    model_name: str = "yolov8n.pt",
    sample_fps: float = 2.0
) -> List[Dict[str, Any]]:
    """
    Runs YOLO subject detection ONLY on the shortlisted clip interval.
    Processes proxy video at a low sample rate (e.g. 2 fps) to keep CPU/RAM low.
    Releases YOLO model memory immediately after execution.
    """
    detections = []

    try:
        from ultralytics import YOLO
        import cv2

        model = YOLO(model_name)
        cap = cv2.VideoCapture(proxy_video)
        fps = cap.get(cv2.CAP_PROP_FPS) or 25.0
        frame_interval = max(1, int(fps / sample_fps))

        start_frame = int(start_sec * fps)
        end_frame = int(end_sec * fps)

        cap.set(cv2.CAP_PROP_POS_FRAMES, start_frame)
        current_frame = start_frame

        while cap.isOpened() and current_frame <= end_frame:
            ret, frame = cap.read()
            if not ret:
                break

            if (current_frame - start_frame) % frame_interval == 0:
                timestamp = current_frame / fps
                results = model(frame, verbose=False, classes=[0]) # class 0 is 'person'

                best_box = None
                max_conf = 0.0

                for r in results:
                    for box in r.boxes:
                        conf = float(box.conf[0])
                        if conf > max_conf:
                            xywhn = box.xywhn[0].tolist() # normalized [center_x, center_y, width, height]
                            max_conf = conf
                            best_box = {
                                "timestamp": round(timestamp, 2),
                                "x": round(xywhn[0], 4),
                                "y": round(xywhn[1], 4),
                                "width": round(xywhn[2], 4),
                                "height": round(xywhn[3], 4),
                                "confidence": round(conf, 3)
                            }

                if best_box:
                    detections.append(best_box)

            current_frame += 1

        cap.release()

        # Explicitly delete model and run garbage collection
        del model
        gc.collect()

    except Exception as e:
        # Fallback: person in center of frame
        pass

    if not detections:
        # Default center framing
        detections.append({
            "timestamp": start_sec,
            "x": 0.5,
            "y": 0.5,
            "width": 0.35,
            "height": 0.70,
            "confidence": 0.5
        })

    return detections
