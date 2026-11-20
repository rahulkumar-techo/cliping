import unittest
import os
import shutil
import tempfile
from downloader import validate_youtube_url, get_video_info, get_ffmpeg_path
from reframer import compute_9_16_crop_window

class TestUserVideo(unittest.TestCase):
    def setUp(self):
        self.url = "https://youtu.be/aF1D_Nk9p9Q?si=6L1UUf7bLfY9KM47"
        self.clean_url = "https://youtu.be/aF1D_Nk9p9Q"

    def test_01_url_validation(self):
        """Verify URL validation recognizes this YouTube video link."""
        self.assertTrue(validate_youtube_url(self.url), "Target URL should be recognized as a valid YouTube video")
        self.assertTrue(validate_youtube_url(self.clean_url))

    def test_02_video_inspection(self):
        """Inspect video metadata: title, duration, resolution, fps."""
        info = get_video_info(self.clean_url)
        self.assertIsNotNone(info, "Should retrieve video metadata")
        title = info.get("title", "")
        duration = info.get("duration", 0)
        width = info.get("width", 1280)
        height = info.get("height", 720)
        fps = info.get("fps", 24)

        print("\n" + "="*50)
        print("VIDEO INSPECTION REPORT:")
        print(f"Title:    {title}")
        print(f"Duration: {duration}s ({duration // 60}m {duration % 60}s)")
        print(f"Format:   {width}x{height} @ {fps} fps")
        print("="*50)

        self.assertIn("Purav Jha", title)
        self.assertEqual(duration, 640)
        self.assertTrue(width >= 1280)
        self.assertTrue(height >= 720)

    def test_03_reframing_geometry_for_video(self):
        """Test vertical 9:16 reframe window on this 1280x720 video."""
        w, h = 1280, 720
        crop_x, crop_y, crop_w, crop_h = compute_9_16_crop_window(w, h, subject_center_x=0.5)

        self.assertEqual(crop_h, 720)
        # 720 * (9/16) = 405 -> 404 (even for H.264)
        self.assertEqual(crop_w, 404)
        self.assertEqual(crop_y, 0)
        # Center: 640 - (404 // 2) = 640 - 202 = 438
        self.assertEqual(crop_x, 438)
        self.assertTrue(0 <= crop_x <= (w - crop_w))

    def test_04_ffmpeg_availability(self):
        """Verify FFmpeg path is resolved (from system or imageio-ffmpeg)."""
        ffmpeg_exe = get_ffmpeg_path()
        self.assertTrue(os.path.exists(ffmpeg_exe), f"FFmpeg should be available at: {ffmpeg_exe}")

if __name__ == "__main__":
    unittest.main()
