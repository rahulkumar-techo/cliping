import sys
import os
import argparse
import sqlite3
import json
import traceback

from pipeline import run_pipeline, emit_event
from cleanup import cleanup_entire_job

def parse_args():
    parser = argparse.ArgumentParser(description="Personal Video AI Clipping Worker")
    parser.add_argument("--job-id", required=True, help="Job ID to process")
    parser.add_argument("--db-path", required=True, help="Path to SQLite database")
    parser.add_argument("--storage-path", required=True, help="Path to storage/jobs")
    return parser.parse_args()

def fetch_job_from_db(db_path: str, job_id: str) -> dict:
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    cursor = conn.cursor()
    cursor.execute("""
        SELECT id, youtube_url, clip_duration, clip_count, aspect_ratio,
               smart_tracking, auto_captions, audio_sync, ai_ranking, auto_publish,
               status, progress, current_stage
        FROM jobs WHERE id = ?
    """, (job_id,))
    row = cursor.fetchone()
    conn.close()
    if not row:
        raise ValueError(f"Job {job_id} not found in database {db_path}")
    return dict(row)

def main():
    args = parse_args()
    job_id = args.job_id
    db_path = args.db_path
    storage_path = args.storage_path

    try:
        job = fetch_job_from_db(db_path, job_id)
        run_pipeline(job, storage_path)
        sys.exit(0)
    except Exception as e:
        tb = traceback.format_exc()
        sys.stderr.write(f"[WORKER-CRASH] {str(e)}\n{tb}\n")
        sys.stderr.flush()
        # Automatically clean up storage on failure
        job_dir = os.path.join(storage_path, job_id)
        cleanup_entire_job(job_dir)
        emit_event("error", code="PIPELINE_FAILED", message=str(e))
        sys.exit(1)

if __name__ == "__main__":
    main()
