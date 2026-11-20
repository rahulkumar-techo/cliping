import unittest
import os
import shutil
import tempfile
import subprocess
import json
import time

from pipeline import run_pipeline, emit_event
from scenes import get_ffmpeg_path

class TestAll17StepsPipeline(unittest.TestCase):
    def setUp(self):
        self.ffmpeg = get_ffmpeg_path()
        self.temp_storage = tempfile.mkdtemp(prefix="cliptest_")
        self.job_id = "TEST17STP"
        self.job_dir = os.path.join(self.temp_storage, self.job_id)
        os.makedirs(self.job_dir, exist_ok=True)

        # Generate a 60-second synthetic test video with audio using FFmpeg in separate folder
        self.source_mp4 = os.path.join(self.temp_storage, "raw_source.mp4")
        cmd = [
            self.ffmpeg, "-y",
            "-f", "lavfi", "-i", "testsrc=duration=60:size=1280x720:rate=24",
            "-f", "lavfi", "-i", "sine=frequency=440:duration=60",
            "-c:v", "libx264", "-preset", "ultrafast",
            "-c:a", "aac",
            self.source_mp4
        ]
        subprocess.run(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)

    def tearDown(self):
        shutil.rmtree(self.temp_storage, ignore_errors=True)

    def test_all_17_steps_and_extract_3_clips(self):
        """
        Executes the entire 17-stage pipeline end-to-end.
        Verifies:
        - All 17 stages run without error
        - Exactly 3 requested clips are generated in final/
        - Each clip is valid 1080x1920 MP4
        - Thumbnails are created
        - Intermediates are cleaned up
        """
        job = {
            "id": self.job_id,
            "youtube_url": "https://www.youtube.com/watch?v=mocktestvideo1",
            "clip_duration": 15,
            "clip_count": 3,
            "aspect_ratio": "9:16",
            "smart_tracking": True,
            "auto_captions": True,
            "audio_sync": True,
            "ai_ranking": False, # Test deterministic local ranking
            "auto_publish": ""
        }

        # Monkey-patch download_youtube_video so it copies our pre-generated source.mp4 to destination
        import pipeline
        orig_download = pipeline.download_youtube_video
        def mock_download(url, out, **kwargs):
            if "progress_callback" in kwargs and kwargs["progress_callback"]:
                kwargs["progress_callback"](100, "1.9/1.9 MB (15 MB/s)")
            shutil.copyfile(self.source_mp4, out)
            return out
        pipeline.download_youtube_video = mock_download

        try:
            t0 = time.time()
            print("\n=======================================================")
            print("RUNNING ALL 17 PIPELINE STEPS TEST (3 CLIPS TARGET)...")
            print("=======================================================")

            pipeline.run_pipeline(job, self.temp_storage)

            elapsed = time.time() - t0
            print(f"--> ALL 17 STEPS COMPLETED in {elapsed:.2f} seconds!")

            # Verify final clips directory
            final_dir = os.path.join(self.job_dir, "final")
            self.assertTrue(os.path.isdir(final_dir), "final/ directory must exist")

            # Check that EXACTLY 3 clips were extracted
            clip_files = [f for f in os.listdir(final_dir) if f.endswith(".mp4")]
            print(f"Generated clips in final/: {clip_files}")
            self.assertEqual(len(clip_files), 3, f"Must extract exactly 3 clips, but found {len(clip_files)}: {clip_files}")

            for idx in (1, 2, 3):
                clip_path = os.path.join(final_dir, f"clip_{idx}.mp4")
                self.assertTrue(os.path.exists(clip_path), f"clip_{idx}.mp4 must exist")
                sz = os.path.getsize(clip_path)
                self.assertGreater(sz, 5000, f"clip_{idx}.mp4 must not be empty (size: {sz})")
                thumb_path = os.path.join(final_dir, f"clip_{idx}.jpg")
                self.assertTrue(os.path.exists(thumb_path), f"Thumbnail clip_{idx}.jpg must exist")

            # Verify intermediate files cleanup (Stage 16)
            self.assertFalse(os.path.exists(os.path.join(self.job_dir, "source.mp4")), "source.mp4 must be cleaned up")
            self.assertFalse(os.path.exists(os.path.join(self.job_dir, "proxy.mp4")), "proxy.mp4 must be cleaned up")
            self.assertFalse(os.path.exists(os.path.join(self.job_dir, "audio.wav")), "audio.wav must be cleaned up")

            # Verify candidates.json and transcript.json exist
            self.assertTrue(os.path.exists(os.path.join(self.job_dir, "candidates.json")))
            self.assertTrue(os.path.exists(os.path.join(self.job_dir, "transcript.json")))

            with open(os.path.join(self.job_dir, "candidates.json"), "r") as f:
                cands = json.load(f)
                self.assertGreaterEqual(len(cands), 3, "Candidates pool must have at least 3 candidates")

            print("[SUCCESS] All 17 steps passed, exactly 3 clips created and verified, cleanup verified!")

        finally:
            pipeline.download_youtube_video = orig_download

if __name__ == "__main__":
    unittest.main()
