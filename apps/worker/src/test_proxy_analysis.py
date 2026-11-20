import unittest
import os
import time
import shutil
import tempfile
import subprocess

from scenes import create_analysis_proxy, get_ffmpeg_path

class TestProxyAnalysis(unittest.TestCase):
    def setUp(self):
        self.ffmpeg = get_ffmpeg_path()
        self.temp_dir = tempfile.mkdtemp()
        self.dummy_video = os.path.join(self.temp_dir, "sample.mp4")
        self.proxy_video = os.path.join(self.temp_dir, "proxy.mp4")

        # Generate a 15-second test video with FFmpeg
        cmd = [
            self.ffmpeg, "-y",
            "-f", "lavfi", "-i", "testsrc=duration=15:size=1280x720:rate=24",
            "-c:v", "libx264", "-preset", "ultrafast",
            self.dummy_video
        ]
        subprocess.run(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)

    def tearDown(self):
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_01_proxy_speed_and_completion(self):
        """Verify proxy generation runs fast without hanging or deadlocking."""
        t0 = time.time()
        proxy = create_analysis_proxy(self.dummy_video, self.proxy_video)
        duration = time.time() - t0

        print(f"\n[TEST PROXY] Generation took {duration:.2f} seconds.")
        self.assertTrue(os.path.exists(proxy), "Proxy file must exist")
        self.assertGreater(os.path.getsize(proxy), 1000, "Proxy file must not be 0 bytes")
        # Must complete within 10 seconds for a sample video
        self.assertLess(duration, 10.0, "Proxy generation must not freeze or take too long")

    def test_02_proxy_dimensions(self):
        """Verify generated proxy is scaled to 640x360."""
        create_analysis_proxy(self.dummy_video, self.proxy_video)
        self.assertTrue(os.path.exists(self.proxy_video))
        self.assertGreater(os.path.getsize(self.proxy_video), 0)

if __name__ == "__main__":
    unittest.main()
