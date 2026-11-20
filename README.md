# ⚡ ClipCraft AI (Personal YouTube → Shorts & Reels Generator)

A personal-use AI-powered studio that converts long-form YouTube videos into high-engagement **9:16 mobile vertical YouTube Shorts & Instagram Reels** with intelligent subject tracking, speech alignment, auto-captions, and AI ranking.

> **Target Machine Profile:**
> - **CPU:** 4 cores
> - **RAM:** 8 GB
> - **Disk:** 32 GB
> - **Concurrency:** `MAX_CONCURRENT_VIDEO_JOBS=1`
> - **Design Philosophy:** Single-user, zero bloated microservices, no Redis, no PostgreSQL, no Node.js/React. Ultra-low CPU, RAM, and disk footprint.

---

## 🏛 Architecture

```
Mustache UI (Vanilla HTML/CSS/JS)
    ↓
Go API Server (net/http + SSE)
    ↓
SQLite Persistent Job Queue
    ↓
Python AI Worker (yt-dlp + FFmpeg + faster-whisper + YOLO + ByteTrack)
    ↓
Cheap Local Candidate Selection (Hook, Speech Density, Audio Energy)
    ↓
Gemini API Semantic Ranking (gemini-2.5-flash with local fallback)
    ↓
Smart 9:16 Subject Reframing (YOLO & ByteTrack only for shortlisted clips)
    ↓
FFmpeg Final 1080x1920 Render & Subtitle Burn
    ↓
Upload to YouTube / Instagram (Optional)
    ↓
Automatic Post-Upload Local File Cleanup
```

---

## 📁 Monorepo Structure

```
/
├── apps/
│   ├── web/                     # Lightweight Mustache Frontend
│   │   ├── templates/           # Server-rendered templates
│   │   │   ├── layout.mustache
│   │   │   ├── create.mustache
│   │   │   ├── processing.mustache
│   │   │   ├── results.mustache
│   │   │   └── settings.mustache
│   │   └── static/
│   │       ├── css/style.css
│   │       └── js/app.js        # Vanilla JS with SSE auto-reconnect
│   ├── api/                     # Go Backend Controller
│   │   ├── cmd/main.go
│   │   ├── internal/
│   │   │   ├── config/          # Environment parser with safety guards
│   │   │   ├── database/        # Pure Go SQLite driver (CGO-free)
│   │   │   ├── jobs/            # Queue manager & single-worker lock
│   │   │   ├── handlers/        # Web & REST API endpoints
│   │   │   ├── sse/             # Real-time event streaming hub
│   │   │   └── cleanup/         # Disk reclamation & TTL cleaner
│   │   └── go.mod
│   └── worker/                  # Python Media & AI Worker
│       ├── src/
│       │   ├── main.py          # Worker entrypoint
│       │   ├── pipeline.py      # 17-stage orchestration
│       │   ├── downloader.py    # yt-dlp 1080p stream ingestion
│       │   ├── audio.py         # 16kHz PCM & RMS audio analyzer
│       │   ├── transcription.py # faster-whisper with memory release
│       │   ├── scenes.py        # 640x360 proxy & scene detection
│       │   ├── candidates.py    # Local heuristic scoring
│       │   ├── ai_ranker.py     # Gemini virality & retention scoring
│       │   ├── detector.py      # YOLO subject detection (shortlist only)
│       │   ├── tracker.py       # ByteTrack with EMA smoothing
│       │   ├── reframer.py      # 9:16 crop calculation
│       │   ├── captions.py      # Mobile-safe ASS subtitles
│       │   ├── renderer.py      # FFmpeg 1080x1920 final encoder
│       │   ├── publisher.py     # YouTube & Instagram API clients
│       │   └── cleanup.py       # Intermediate file cleaner
│       └── requirements.txt
├── packages/
│   ├── shared/models.json       # Model specs & limits
│   └── config/default.env
├── data/                        # SQLite storage (app.db)
├── storage/jobs/                # Isolated per-job temporary workspace
├── scripts/
│   ├── setup.sh                 # VPS setup script
│   ├── test.sh                  # Test runner (Go & Python)
│   └── cleanup.py               # TTL purge utility
├── docker/
│   ├── Dockerfile               # Production multi-stage Docker build
│   └── nginx.conf               # Optional reverse proxy with SSE tuning
├── docker-compose.yml
├── Makefile
├── .env.example
├── .gitignore
└── README.md
```

---

## ⚡ 17 Pipeline Processing Stages

1. **Validating URL**: Ensures valid YouTube link and verifies >= 8 GB free disk.
2. **Downloading**: yt-dlp fetches source video (caps at 1080p to prevent disk bloat).
3. **Extracting audio**: FFmpeg extracts 16kHz mono WAV for Whisper.
4. **Creating analysis proxy**: Downsamples source to 640x360 proxy for lightweight visual operations.
5. **Transcribing**: faster-whisper runs int8 CPU transcription, then frees model RAM immediately.
6. **Detecting scenes**: Identifies camera cuts to avoid awkward clip starts/ends.
7. **Analyzing audio**: Extracts RMS energy, silence segments, and speech density.
8. **Generating candidates**: Identifies 10-20 windows respecting target duration (+/- 3s sentence alignment).
9. **AI ranking**: Gemini evaluates hook, payoff, and retention; fallback to local heuristic score if unavailable.
10. **Detecting subjects**: YOLOv8n loads *only* for the top 3-5 shortlisted clips to locate main subjects.
11. **Tracking subject**: ByteTrack traces the speaker; smoothed using EMA to avoid camera jumps.
12. **Smart reframing**: Computes 9:16 crop boundaries clamped inside source dimensions.
13. **Generating captions**: Formats mobile safe-margin ASS subtitles.
14. **Rendering**: FFmpeg renders final 1080x1920 H.264/AAC clip with faststart.
15. **Uploading**: Publishes to YouTube Shorts and/or Instagram Reels if configured.
16. **Cleanup**: Automatically deletes source video, proxy, and audio WAV.
17. **Completed**: Final clips ready for browser playback or download.

---

## 🚀 Quick Start on a Linux / VPS Machine

### 1. Prerequisites
- **OS:** Ubuntu 22.04 / 24.04 LTS or Debian 12
- **Tools:** `ffmpeg`, `python3` (3.10+), `sqlite3`, `curl`

### 2. Automatic Setup
Clone the repository and run the setup script:
```bash
git clone <repo-url> clipping
cd clipping
chmod +x scripts/*.sh
./scripts/setup.sh
```

### 3. Configure Environment
```bash
cp .env.example .env
nano .env
```
Key settings to configure:
- `GEMINI_API_KEY`: Your Google Gemini API Key. (Get one free at https://aistudio.google.com).
- `GEMINI_MODEL`: Default `gemini-2.5-flash`.

### 4. Run the Server
```bash
make run
```
Open `http://<your-vps-ip>:8080` in your web browser.

---

## 🐳 Docker Deployment

To run in isolated containers with CPU (4 cores) and RAM (7 GB) limits enforced:

```bash
# 1. Start application
docker compose up --build -d

# 2. Check logs
docker compose logs -f app

# 3. Stop application
docker compose down
```

To run with the optional Nginx reverse proxy on port 80:
```bash
docker compose --profile proxy up --build -d
```

---

## 🧠 Memory & Model Lifecycle (Strict Low-Resource Budget)

On an 8 GB RAM server, keeping multiple AI models resident causes Out-Of-Memory (OOM) crashes. ClipCraft AI uses a **sequential model lifecycle**:

```
Stage 1-4:   No AI models in memory (~120 MB RAM)
Stage 5:     faster-whisper (int8) loaded (~800 MB RAM) → transcribed → freed (gc.collect)
Stage 6-8:   Local heuristics (~150 MB RAM)
Stage 9:     Gemini API text call (0 MB local RAM)
Stage 10-11: YOLOv8 nano loaded (~300 MB RAM) → tracked → freed (gc.collect)
Stage 12-14: FFmpeg single-process render (2 threads)
Stage 15-16: Intermediate files deleted from disk
```

---

## 🔑 Publishing Setup

### YouTube Shorts
1. Create a Google Cloud Project and enable **YouTube Data API v3**.
2. Create an OAuth 2.0 Client ID.
3. Set `YOUTUBE_CLIENT_ID` and `YOUTUBE_CLIENT_SECRET` in `.env`.
4. Videos are uploaded with `#Shorts` in the title and description.

### Instagram Reels
1. Register on Meta for Developers and create an App with Instagram Graph API permissions.
2. Set `INSTAGRAM_APP_ID` and `INSTAGRAM_APP_SECRET` in `.env`.

---

## 🧪 Testing

Run both Go and Python test suites:
```bash
make test
```
Or individually:
```bash
# Go API tests
cd apps/api && go test -v ./...

# Python worker tests
cd apps/worker/src && python test_pipeline.py
```

---

## 🧹 Disk Storage & Cleanup Rules

- Every job runs in an isolated directory: `storage/jobs/<job-id>/`.
- Before downloading, available disk space is verified against `MIN_FREE_DISK_GB=8`.
- Once clips are rendered or published, heavy intermediates (`source.mp4`, `proxy.mp4`, `audio.wav`) are deleted.
- Abandoned or expired jobs older than `JOB_TTL_HOURS=12` are purged automatically by the background cleaner service or via `python scripts/cleanup.py`.

---

## 📄 License
Personal-Use Only. Free to use, modify, and host on your personal hardware or VPS.
