// ClipCraft AI - Lightweight Vanilla JS Frontend

document.addEventListener("DOMContentLoaded", () => {
    initStatsPoller();
    initCreateForm();
    initProcessingPage();
    initSettingsForm();
});

// Periodic System Stats Poller
function initStatsPoller() {
    const updateStats = async () => {
        try {
            const res = await fetch("/health");
            if (!res.ok) return;
            const data = await res.json();
            const ramElem = document.getElementById("statRAM");
            const queueElem = document.getElementById("statQueue");
            if (ramElem && data.ram_alloc_mb !== undefined) {
                ramElem.textContent = `${data.ram_alloc_mb} MB`;
            }
            if (queueElem && data.queue_length !== undefined) {
                queueElem.textContent = data.queue_length;
            }
        } catch (_) {}
    };
    const cleanBtn = document.getElementById("headerCleanBtn");
    if (cleanBtn) {
        cleanBtn.addEventListener("click", async () => {
            if (!confirm("Reclaim disk space by purging all temporary files across jobs?")) return;
            const orig = cleanBtn.textContent;
            cleanBtn.disabled = true;
            cleanBtn.textContent = "🧹 Cleaning...";
            try {
                const res = await fetch("/api/cleanup", { method: "POST" });
                const data = await res.json();
                if (res.ok) {
                    cleanBtn.textContent = `✓ Freed ${data.freed_mb} MB!`;
                    cleanBtn.style.color = "var(--accent-primary)";
                    setTimeout(() => {
                        cleanBtn.textContent = orig;
                        cleanBtn.style.color = "";
                        cleanBtn.disabled = false;
                    }, 3000);
                } else {
                    alert(data.error || "Cleanup failed");
                    cleanBtn.textContent = orig;
                    cleanBtn.disabled = false;
                }
            } catch (err) {
                alert(`Cleanup error: ${err.message}`);
                cleanBtn.textContent = orig;
                cleanBtn.disabled = false;
            }
        });
    }

    updateStats();
    setInterval(updateStats, 10000);
}

// Create Job Form
function initCreateForm() {
    const form = document.getElementById("createJobForm");
    if (!form) return;

    const durationSelect = document.getElementById("clipDuration");
    const customDuration = document.getElementById("customDuration");
    const autoPublishToggle = document.getElementById("autoPublishToggle");
    const publishingTargets = document.getElementById("publishingTargets");
    const errorBox = document.getElementById("formError");
    const submitBtn = document.getElementById("submitBtn");

    if (durationSelect && customDuration) {
        durationSelect.addEventListener("change", (e) => {
            if (e.target.value === "custom") {
                customDuration.classList.remove("hidden");
                customDuration.required = true;
            } else {
                customDuration.classList.add("hidden");
                customDuration.required = false;
            }
        });
    }

    if (autoPublishToggle && publishingTargets) {
        autoPublishToggle.addEventListener("change", (e) => {
            if (e.target.checked) {
                publishingTargets.classList.remove("hidden");
            } else {
                publishingTargets.classList.add("hidden");
            }
        });
    }

    form.addEventListener("submit", async (e) => {
        e.preventDefault();
        errorBox.classList.add("hidden");
        errorBox.textContent = "";

        const youtubeUrl = document.getElementById("youtubeUrl").value.trim();
        const ytPattern = /^(https?:\/\/)?(www\.)?(youtube\.com\/watch\?v=|youtu\.be\/|youtube\.com\/shorts\/)([a-zA-Z0-9_-]{11})/;
        if (!ytPattern.test(youtubeUrl)) {
            errorBox.textContent = "Please enter a valid YouTube video URL.";
            errorBox.classList.remove("hidden");
            return;
        }

        let duration = parseInt(durationSelect.value, 10);
        if (durationSelect.value === "custom") {
            duration = parseInt(customDuration.value, 10) || 30;
        }

        const count = parseInt(document.getElementById("clipCount").value, 10) || 3;
        const smartTracking = document.getElementById("smartTracking").checked;
        const autoCaptions = document.getElementById("autoCaptions").checked;
        const audioSync = document.getElementById("audioSync").checked;
        const aiRanking = document.getElementById("aiRanking").checked;

        let autoPublish = "";
        if (autoPublishToggle && autoPublishToggle.checked) {
            const targets = [];
            document.querySelectorAll("input[name='publish_target']:checked").forEach(cb => {
                targets.push(cb.value);
            });
            autoPublish = targets.join(",");
        }

        submitBtn.disabled = true;
        submitBtn.querySelector(".btn-text").textContent = "Submitting Job...";

        try {
            const res = await fetch("/api/jobs", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({
                    youtube_url: youtubeUrl,
                    clip_duration: duration,
                    clip_count: count,
                    aspect_ratio: "9:16",
                    smart_tracking: smartTracking,
                    auto_captions: autoCaptions,
                    audio_sync: audioSync,
                    ai_ranking: aiRanking,
                    auto_publish: autoPublish
                })
            });

            const data = await res.json();
            if (!res.ok) {
                throw new Error(data.error || "Failed to create job");
            }

            if (data.duplicate) {
                window.location.href = `/jobs/${data.id}/results`;
            } else {
                window.location.href = `/jobs/${data.id}`;
            }
        } catch (err) {
            errorBox.textContent = err.message;
            errorBox.classList.remove("hidden");
            submitBtn.disabled = false;
            submitBtn.querySelector(".btn-text").textContent = "Generate Clips";
        }
    });
}

// Processing Page with SSE Real-time Updates
function initProcessingPage() {
    const procCard = document.querySelector(".processing-card");
    if (!procCard) return;

    const jobId = procCard.dataset.jobId;
    const stageText = document.getElementById("currentStageText");
    const percentText = document.getElementById("currentPercentText");
    const progressBar = document.getElementById("progressBarFill");
    const terminalLogs = document.getElementById("terminalLogs");
    const sseDot = document.getElementById("sseStatusDot");
    const cancelBtn = document.getElementById("cancelJobBtn");
    const copyLogsBtn = document.getElementById("copyLogsBtn");
    const clearLogsBtn = document.getElementById("clearLogsBtn");

    if (copyLogsBtn) {
        copyLogsBtn.addEventListener("click", () => {
            const text = terminalLogs ? terminalLogs.textContent : "";
            if (!text) return;
            navigator.clipboard.writeText(text).then(() => {
                const orig = copyLogsBtn.textContent;
                copyLogsBtn.textContent = "✓ Copied!";
                copyLogsBtn.style.color = "var(--accent-primary)";
                setTimeout(() => {
                    copyLogsBtn.textContent = orig;
                    copyLogsBtn.style.color = "";
                }, 2000);
            }).catch(err => {
                console.error("Clipboard copy failed: ", err);
            });
        });
    }

    if (clearLogsBtn) {
        clearLogsBtn.addEventListener("click", () => {
            if (terminalLogs) {
                terminalLogs.textContent = "";
            }
        });
    }

    if (cancelBtn) {
        cancelBtn.addEventListener("click", async () => {
            if (!confirm("Are you sure you want to cancel this job?")) return;
            cancelBtn.disabled = true;
            try {
                const res = await fetch(`/api/jobs/${jobId}/cancel`, { method: "POST" });
                const d = await res.json();
                if (res.ok) {
                    appendLog("Cancellation", "WARN", "Job cancelled. Redirecting...");
                    setTimeout(() => { window.location.href = "/create"; }, 1500);
                } else {
                    alert(d.error || "Cancellation failed");
                    cancelBtn.disabled = false;
                }
            } catch (err) {
                alert(err.message);
                cancelBtn.disabled = false;
            }
        });
    }

    const stagesList = document.querySelectorAll(".stage-item");
    const updateActiveStageInList = (currentStage) => {
        let reached = true;
        stagesList.forEach(item => {
            const stageName = item.dataset.stage;
            const slug = stageName.replace(/\s+/g, "-");
            const tag = document.getElementById(`tag-${slug}`);
            const track = document.getElementById(`track-${slug}`);
            const fill = document.getElementById(`fill-${slug}`);

            if (stageName === currentStage) {
                item.classList.add("active");
                item.classList.remove("completed");
                if (tag) {
                    tag.textContent = "Running";
                    tag.className = "stage-tag active";
                }
                if (track) track.classList.remove("hidden");
                reached = false;
            } else if (reached) {
                item.classList.add("completed");
                item.classList.remove("active");
                if (tag) {
                    tag.textContent = "✓ Done";
                    tag.className = "stage-tag completed";
                }
                if (track) track.classList.remove("hidden");
                if (fill) fill.style.width = "100%";
            } else {
                item.classList.remove("completed", "active");
                if (tag) {
                    tag.textContent = "Pending";
                    tag.className = "stage-tag pending";
                }
                if (track) track.classList.add("hidden");
            }
        });
    };

    const appendLog = (stage, level, msg, timestamp) => {
        const time = timestamp || new Date().toLocaleTimeString();
        const line = `[${time}] [${level}] [${stage}] ${msg}\n`;
        terminalLogs.textContent += line;
        terminalLogs.scrollTop = terminalLogs.scrollHeight;
    };

    const errorBanner = document.getElementById("jobErrorBanner");
    const errorCode = document.getElementById("jobErrorCode");
    const errorMsg = document.getElementById("jobErrorMessage");
    const retryBtn = document.getElementById("retryJobBtn");

    const showJobFailed = (code, message) => {
        if (errorBanner) {
            if (errorCode) errorCode.textContent = `Job Failed: [${code || 'ERROR'}]`;
            if (errorMsg) errorMsg.textContent = message || "An unexpected error occurred during processing.";
            errorBanner.classList.remove("hidden");
        }
        if (cancelBtn) cancelBtn.classList.add("hidden");
        if (retryBtn) retryBtn.classList.remove("hidden");
        if (sseDot) {
            sseDot.textContent = "● Job Failed";
            sseDot.style.color = "var(--accent-danger)";
        }
        if (progressBar) {
            progressBar.style.background = "var(--accent-danger)";
        }
    };

    // Periodic state check fallback
    const pollInterval = setInterval(async () => {
        try {
            const res = await fetch(`/api/jobs/${jobId}`);
            if (!res.ok) return;
            const job = await res.json();
            if (job.status === "COMPLETED") {
                clearInterval(pollInterval);
                window.location.href = `/jobs/${jobId}/results`;
            } else if (job.status === "FAILED") {
                clearInterval(pollInterval);
                showJobFailed(job.error_code, job.error_message);
                if (eventSource) eventSource.close();
            } else if (job.status === "CANCELLED") {
                clearInterval(pollInterval);
                window.location.href = "/create";
            }
        } catch (_) {}
    }, 3000);

    // SSE Connection with auto-reconnect
    let eventSource = null;
    const connectSSE = () => {
        if (eventSource) {
            eventSource.close();
        }
        eventSource = new EventSource(`/api/jobs/${jobId}/events`);

        eventSource.onopen = () => {
            if (sseDot) {
                sseDot.textContent = "● SSE Live";
                sseDot.style.color = "var(--accent-primary)";
            }
        };

        eventSource.addEventListener("stage", (e) => {
            const data = JSON.parse(e.data);
            if (stageText) stageText.textContent = data.stage;
            if (percentText && data.progress !== undefined) percentText.textContent = `${data.progress}%`;
            if (progressBar && data.progress !== undefined) progressBar.style.width = `${data.progress}%`;
            updateActiveStageInList(data.stage);
        });

        eventSource.addEventListener("progress", (e) => {
            const data = JSON.parse(e.data);
            if (percentText) percentText.textContent = `${data.progress}%`;
            if (progressBar) progressBar.style.width = `${data.progress}%`;
        });

        eventSource.addEventListener("step_progress", (e) => {
            const data = JSON.parse(e.data);
            const slug = data.stage.replace(/\s+/g, "-");
            const tag = document.getElementById(`tag-${slug}`);
            const track = document.getElementById(`track-${slug}`);
            const fill = document.getElementById(`fill-${slug}`);
            const detail = document.getElementById(`detail-${slug}`);

            if (tag) {
                tag.textContent = `${data.percent}%`;
                tag.className = "stage-tag active";
            }
            if (track) track.classList.remove("hidden");
            if (fill) fill.style.width = `${data.percent}%`;
            if (detail && data.detail) {
                detail.textContent = data.detail;
                detail.classList.remove("hidden");
            }
            if (stageText && data.detail) {
                stageText.textContent = `${data.stage} • ${data.detail}`;
            }
        });

        eventSource.addEventListener("log", (e) => {
            const data = JSON.parse(e.data);
            appendLog(data.stage, data.level, data.message, data.timestamp);
        });

        eventSource.addEventListener("completed", () => {
            clearInterval(pollInterval);
            appendLog("Completed", "INFO", "Job finished successfully! Redirecting to results...");
            if (progressBar) progressBar.style.width = "100%";
            if (percentText) percentText.textContent = "100%";
            setTimeout(() => {
                window.location.href = `/jobs/${jobId}/results`;
            }, 800);
            eventSource.close();
        });

        eventSource.addEventListener("error", (e) => {
            if (e.data) {
                try {
                    const data = JSON.parse(e.data);
                    appendLog("Error", "ERROR", `[${data.code}] ${data.message}`);
                    showJobFailed(data.code, data.message);
                    clearInterval(pollInterval);
                    eventSource.close();
                    return;
                } catch (_) {}
            }
            if (sseDot) {
                sseDot.textContent = "○ SSE Reconnecting...";
                sseDot.style.color = "var(--accent-warning)";
            }
        });
    };

    connectSSE();
}

// Publish Clip Action (YouTube Shorts / Instagram Reels)
async function publishClip(clipId, platform) {
    const btn = event.target;
    const origText = btn.textContent;
    btn.disabled = true;
    btn.textContent = "Publishing...";

    try {
        const res = await fetch(`/api/clips/${clipId}/publish/${platform}`, {
            method: "POST"
        });
        const data = await res.json();
        if (!res.ok) {
            throw new Error(data.error || "Publishing failed");
        }
        btn.textContent = "✓ Published!";
        btn.style.backgroundColor = "var(--accent-primary)";
        alert(`Successfully published to ${platform}! External ID: ${data.external_id}`);
    } catch (err) {
        alert(`Publishing Error: ${err.message}`);
        btn.disabled = false;
        btn.textContent = origText;
    }
}

// Settings Form
function initSettingsForm() {
    const form = document.getElementById("settingsForm");
    if (!form) return;

    const alertBox = document.getElementById("settingsAlert");
    form.addEventListener("submit", async (e) => {
        e.preventDefault();
        alertBox.classList.add("hidden");
        alertBox.className = "alert hidden";

        const formData = new FormData(form);
        const payload = {};
        formData.forEach((val, key) => {
            payload[key] = val;
        });

        try {
            const res = await fetch("/api/settings", {
                method: "PUT",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify(payload)
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || "Save failed");

            alertBox.textContent = "Settings saved successfully!";
            alertBox.classList.add("alert-success");
            alertBox.classList.remove("hidden");
        } catch (err) {
            alertBox.textContent = err.message;
            alertBox.classList.add("alert-danger");
            alertBox.classList.remove("hidden");
        }
    });
}
