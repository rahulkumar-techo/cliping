import os
import shutil

INTERMEDIATES = [
    "source.mp4",
    "proxy.mp4",
    "audio.wav",
    "frames",
    "temp_audio.wav"
]

def cleanup_intermediate_files(job_dir: str):
    """
    Deletes large source, proxy, and audio analysis files once rendering is finished.
    Keeps final/ clips and metadata JSONs.
    """
    for item in INTERMEDIATES:
        path = os.path.join(job_dir, item)
        if os.path.isfile(path):
            try:
                os.remove(path)
            except Exception as e:
                print(f"[CLEANUP] Could not delete {path}: {e}")
        elif os.path.isdir(path):
            try:
                shutil.rmtree(path, ignore_errors=True)
            except Exception as e:
                print(f"[CLEANUP] Could not remove directory {path}: {e}")

def cleanup_entire_job(job_dir: str):
    """Deletes entire job directory and all its contents."""
    if os.path.exists(job_dir):
        shutil.rmtree(job_dir, ignore_errors=True)
