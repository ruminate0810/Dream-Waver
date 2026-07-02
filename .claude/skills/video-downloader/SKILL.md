---
name: video-downloader
description: >
  Download videos from multiple platforms. Use this skill when the user wants to:
  download a video, save a video, get a video from YouTube, TikTok, Instagram, Twitter/X,
  or any other video platform. Also trigger when the user mentions 下载视频, 保存视频,
  YouTube视频下载, TikTok下载, 抖音下载, Instagram视频, 推特视频, 视频下载器,
  or provides a video URL and asks to download it.
  Handles the full workflow: URL detection, platform routing, multi-strategy download,
  and file output.
version: 1.0.0
metadata:
  openclaw:
    requires:
      bins:
        - python3
        - node
---

# Video Downloader

Download videos from YouTube, TikTok, Instagram, Twitter/X, and 1000+ other platforms to local MP4 files.

## Prerequisites

- **Python 3.9+** with `requests` (`pip3 install requests`)
- **Node.js 18+** (for YouTube; auto-detected from PATH, homebrew, nvm, volta, fnm)
- **yt-dlp** (for non-YouTube platforms; `pip3 install yt-dlp`)

YouTube 下载只需 Node.js 或 yt-dlp 其中之一即可。首次下载 YouTube 视频时会自动安装 `youtubei.js` 到 skill 本地 `node_modules/`。

## Workflow

### Phase 0 — Collect Input

从用户获取：
- **视频 URL**（必需）
- **输出目录**（可选，默认 `./output`）
- **文件名**（可选，默认从视频标题生成）

### Phase 1 — Download

运行下载脚本（脚本内部自动处理平台检测和策略选择）：

```bash
python3 ${CLAUDE_SKILL_DIR}/scripts/download_video.py \
  --url "<VIDEO_URL>" \
  --output-dir <OUTPUT_DIR> \
  --filename <FILENAME>
```

参数说明：
- `--url`（必需）：视频 URL
- `--output-dir`：输出目录（默认 `./output`）
- `--filename`：输出文件名，不含扩展名（默认从视频标题生成）
- `--info-only`：只获取元数据（标题、作者、时长），不下载
- `--verbose`：开启详细日志

### Phase 2 — Report

向用户报告：
- 下载文件路径
- 文件大小
- 检测到的平台

## Platform & Strategy Reference

| 平台 | 方法 | 说明 |
|------|------|------|
| YouTube | Node.js + youtubei.js → yt-dlp fallback | 自包含，自动处理 n-param decipher |
| TikTok / 抖音 | tikwm API | 快速，支持 HD |
| Instagram | yt-dlp | Reels 和视频帖子 |
| Twitter / X | yt-dlp | 视频推文和 GIF |
| Bilibili / Vimeo / 其他 | yt-dlp | 1000+ 站点 |

YouTube 策略链（自动按顺序尝试直到成功）：
1. Node.js + youtubei.js（首选，自包含）
2. yt-dlp + 浏览器 cookies
3. yt-dlp plain
4. yt-dlp + 自动检测的 SOCKS5 代理

## Error Handling

| 问题 | 原因 | 修复 |
|-----|------|------|
| YouTube 403 / n-param | 节流保护 | Node.js 策略自动处理；确保 Node.js 18+ 可用 |
| YouTube SABR streaming | 新流媒体格式 | Node.js 策略绕过 |
| YouTube 需登录 | 年龄限制 | yt-dlp + 浏览器 cookies |
| TikTok 403 | 反爬 | tikwm API 自动处理 |
| 地区限制 | 地理封锁 | 自动检测本地 SOCKS5 代理 |
| Node.js 未找到 | 未安装 | 自动降级到 yt-dlp |
| 所有策略失败 | 网络/URL 问题 | 提示用户检查 URL 或手动下载 |
