import unittest
import os
import shutil
import tempfile

from candidates import generate_candidates
from reframer import compute_9_16_crop_window
from tracker import smooth_tracking_trajectory, get_dominant_subject_center
from captions import format_timestamp_ass, generate_ass_subtitles
from cleanup import cleanup_intermediate_files

class TestWorkerPipeline(unittest.TestCase):

    def test_crop_calculations(self):
        # 1920x1080 source video
        source_w, source_h = 1920, 1080

        # Center subject (x=0.5)
        crop_x, crop_y, crop_w, crop_h = compute_9_16_crop_window(source_w, source_h, 0.5, 0.5)
        self.assertEqual(crop_h, 1080)
        # 1080 * (9/16) = 607.5 -> 606 (even)
        self.assertEqual(crop_w, 606)
        self.assertTrue(0 <= crop_x <= (source_w - crop_w))
        self.assertEqual(crop_y, 0)

        # Subject at extreme left boundary (x=0.0) -> crop clamped to 0
        crop_x_left, _, _, _ = compute_9_16_crop_window(source_w, source_h, 0.0, 0.5)
        self.assertEqual(crop_x_left, 0)

        # Subject at extreme right boundary (x=1.0) -> crop clamped to source_w - crop_w
        crop_x_right, _, _, _ = compute_9_16_crop_window(source_w, source_h, 1.0, 0.5)
        self.assertEqual(crop_x_right, source_w - crop_w)

    def test_tracking_smoothing(self):
        # Jerky detections
        raw_detections = [
            {"timestamp": 0.0, "x": 0.20, "y": 0.5, "width": 0.3, "height": 0.6, "confidence": 0.9},
            {"timestamp": 1.0, "x": 0.80, "y": 0.5, "width": 0.3, "height": 0.6, "confidence": 0.9},
            {"timestamp": 2.0, "x": 0.85, "y": 0.5, "width": 0.3, "height": 0.6, "confidence": 0.9},
        ]
        smoothed = smooth_tracking_trajectory(raw_detections, alpha=0.3)
        self.assertEqual(len(smoothed), 3)
        # First point remains 0.20
        self.assertAlmostEqual(smoothed[0]["x"], 0.20, places=2)
        # Second point smoothed: 0.3*0.8 + 0.7*0.2 = 0.24 + 0.14 = 0.38
        self.assertAlmostEqual(smoothed[1]["x"], 0.38, places=2)
        # Center calculation
        center = get_dominant_subject_center(smoothed)
        self.assertTrue(0.2 <= center <= 0.85)

    def test_candidate_generation_and_scoring(self):
        segments = [
            {"start": 0.0, "end": 4.5, "text": "Nobody expected this shocking secret!"},
            {"start": 4.5, "end": 15.0, "text": "We worked day and night to optimize resource constraints."},
            {"start": 15.0, "end": 28.5, "text": "And the conclusion is truly spectacular."}
        ]
        scenes = [{"start": 0.0, "end": 15.0}, {"start": 15.0, "end": 30.0}]
        audio_features = {
            "window_sec": 1.0,
            "max_rms": 1000.0,
            "mean_rms": 500.0,
            "rms_timeline": [600.0] * 30
        }

        candidates = generate_candidates(segments, scenes, audio_features, target_duration=28)
        self.assertTrue(len(candidates) >= 1)
        top = candidates[0]
        self.assertTrue(0.0 <= top["local_score"] <= 100.0)
        self.assertTrue(top["hook_score"] > 50.0) # Contains "nobody", "secret", "shocking"
        self.assertTrue(top["duration"] >= 20.0)

    def test_captions_formatting(self):
        self.assertEqual(format_timestamp_ass(0.0), "0:00:00.00")
        self.assertEqual(format_timestamp_ass(65.25), "0:01:05.25")
        self.assertEqual(format_timestamp_ass(3661.50), "1:01:01.50")

        tmp_dir = tempfile.mkdtemp()
        try:
            ass_path = os.path.join(tmp_dir, "test.ass")
            segments = [
                {"start": 10.0, "end": 14.5, "text": "Hello world!"},
                {"start": 15.0, "end": 20.0, "text": "ClipCraft AI subtitle test"}
            ]
            generate_ass_subtitles(segments, clip_start=10.0, clip_end=20.0, output_ass_path=ass_path)
            self.assertTrue(os.path.exists(ass_path))
            with open(ass_path, "r", encoding="utf-8") as f:
                content = f.read()
            self.assertIn("PlayResX: 1080", content)
            self.assertIn("Hello world!", content)
            self.assertIn("ClipCraft AI subtitle test", content)
        finally:
            shutil.rmtree(tmp_dir, ignore_errors=True)

    def test_cleanup_intermediates(self):
        tmp_dir = tempfile.mkdtemp()
        try:
            # Create files
            source = os.path.join(tmp_dir, "source.mp4")
            proxy = os.path.join(tmp_dir, "proxy.mp4")
            audio = os.path.join(tmp_dir, "audio.wav")
            final_dir = os.path.join(tmp_dir, "final")
            os.makedirs(final_dir, exist_ok=True)
            clip = os.path.join(final_dir, "clip_1.mp4")

            for f in [source, proxy, audio, clip]:
                with open(f, "w") as fp:
                    fp.write("dummy")

            cleanup_intermediate_files(tmp_dir)

            # Source, proxy, audio must be gone
            self.assertFalse(os.path.exists(source))
            self.assertFalse(os.path.exists(proxy))
            self.assertFalse(os.path.exists(audio))

            # Final clip must be preserved
            self.assertTrue(os.path.exists(clip))
        finally:
            shutil.rmtree(tmp_dir, ignore_errors=True)

if __name__ == "__main__":
    unittest.main()
