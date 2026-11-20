#!/usr/bin/env python3
"""
CLI tool for manual or cron-based disk cleanup.
Usage: python scripts/cleanup.py [--ttl-hours 12] [--storage-path storage/jobs] [--db-path data/app.db]
"""
import os
import sys
import argparse
import time
import shutil
import sqlite3

def run_cleanup(storage_path: str, db_path: str, ttl_hours: int = 12):
    cutoff_time = time.time() - (ttl_hours * 3600)
    print(f"[CLEANUP] Scanning {storage_path} for files older than {ttl_hours} hours...")

    purged_count = 0
    if os.path.exists(storage_path):
        for entry in os.listdir(storage_path):
            job_dir = os.path.join(storage_path, entry)
            if os.path.isdir(job_dir):
                mtime = os.path.getmtime(job_dir)
                if mtime < cutoff_time:
                    try:
                        shutil.rmtree(job_dir)
                        purged_count += 1
                        print(f"[CLEANUP] Purged expired job folder: {entry}")
                    except Exception as e:
                        print(f"[CLEANUP] Error removing {job_dir}: {e}")

    # Remove corresponding records from DB if db exists
    if os.path.exists(db_path):
        try:
            conn = sqlite3.connect(db_path)
            cursor = conn.cursor()
            cursor.execute("""
                DELETE FROM jobs
                WHERE status IN ('COMPLETED', 'FAILED', 'CANCELLED')
                  AND datetime(updated_at) < datetime('now', '-' || ? || ' hours')
            """, (ttl_hours,))
            deleted_rows = cursor.rowcount
            conn.commit()
            conn.close()
            print(f"[CLEANUP] Cleaned {deleted_rows} expired job records from database.")
        except Exception as e:
            print(f"[CLEANUP] DB cleanup error: {e}")

    print(f"[CLEANUP] Complete. Removed {purged_count} directories.")

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Purge old job files and database records")
    parser.add_argument("--storage-path", default="storage/jobs", help="Jobs directory")
    parser.add_argument("--db-path", default="data/app.db", help="SQLite database path")
    parser.add_argument("--ttl-hours", type=int, default=12, help="Hours before job is considered expired")
    args = parser.parse_args()

    run_cleanup(args.storage_path, args.db_path, args.ttl_hours)
