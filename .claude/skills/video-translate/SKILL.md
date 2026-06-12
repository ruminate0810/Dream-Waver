---
name: video-translate
description: >
  Translate and dub videos into another language using DreamAPI Video Translate.
  Replaces original audio with AI-dubbed audio in the target language with lip-sync.
  Use this skill when the user wants to: translate a video, dub a video,
  convert video language, voice-over in another language, or mentions
  视频翻译, 视频配音, 视频多语言, video dubbing, translate audio,
  AI配音, 语音翻译, 影片翻译, 동영상번역, ビデオ翻訳.
  Supports: zh, en, ja, ko, es, fr, de, pt, ru, ar, hi, it.
  Accepts local video files or video URLs (YouTube, etc.).
---

# Video Translate

将视频翻译配音成另一种语言。替换原始音频为 AI 配音，支持唇形同步。

## Supported Languages

| Code | Language | Code | Language |
|------|----------|------|----------|
| zh | Chinese | fr | French |
| en | English | de | German |
| ja | Japanese | pt | Portuguese |
| ko | Korean | ru | Russian |
| es | Spanish | ar | Arabic |
| hi | Hindi | it | Italian |

## Prerequisites

- `python3 3.9+` with `requests`
- `Node.js 18+`（用于下载 YouTube 视频，自动检测路径，首次运行自动安装 youtubei.js）
- `ffprobe`（可选，用于时长检测）
- **Newport AI API key**：检查环境变量 `NEWPORT_API_KEY`（必须设置，不再内置兜底 key）

## Workflow

### Phase 0 — Collect Input

从用户获取：
- **视频来源**（必需）：本地文件路径 或 视频 URL
- **源语言** `sourceLang`（默认 `zh`）
- **目标语言** `targetLang`（必需，逗号分隔可批量：`en,ja,ko`）

如果用户只说"翻译成中文"而没给源语言，根据视频来源推断（YouTube 英文视频 → `sourceLang=en`）。

### Phase 1 — Translate

运行翻译脚本。脚本内部自动处理完整流程：
- 如果输入是 YouTube URL → 自动下载到本地（内置 Node.js + youtubei.js）
- 本地文件 → 自动上传到公共 URL
- 提交翻译 API → 轮询等待（3-8 分钟）→ 下载结果

**单语言翻译：**
```bash
python3 ${CLAUDE_SKILL_DIR}/scripts/translate_video.py \
  --input <VIDEO_FILE_OR_URL> \
  --source-lang <SOURCE> \
  --target-lang <TARGET>
```

**批量翻译（多语言并行，总耗时约等于单次）：**
```bash
python3 ${CLAUDE_SKILL_DIR}/scripts/translate_video.py \
  --input <VIDEO_FILE_OR_URL> \
  --source-lang <SOURCE> \
  --target-lang en,ja,ko
```

参数说明：
- `--input`（必需）：本地视频文件路径 **或** YouTube URL（自动下载）
- `--source-lang`：源语言代码（默认 `zh`）
- `--target-lang`（必需）：目标语言代码，逗号分隔可批量
- `--output`：输出文件路径（默认 `<input>_<lang>.mp4`）

### Phase 2 — Report

向用户报告：
- 输出文件路径
- 文件大小
- 源语言 → 目标语言
- 处理耗时

## Pipeline Integration

| 场景 | 流程 |
|------|------|
| YouTube → 中文配音 | `video-translate --input <URL>`（内置下载） |
| 本地视频 → 英文配音 | `video-translate --input video.mp4 --target-lang en` |
| 一次翻译多语言 | `video-translate --input video.mp4 --target-lang en,ja,ko` |

## Error Handling

| 问题 | 原因 | 修复 |
|-----|------|------|
| 短视频失败（<30s） | API 限制 | 使用 1 分钟以上的视频 |
| file download failed | API 无法访问 fileUrl | 确保文件已上传到公共 URL（脚本自动处理） |
| Status 4 无原因 | API 内部错误 | 重试一次；若仍失败，换格式或视频 |
| 超时 15 分钟 | 视频过长或 API 过载 | 手动查询 taskId 状态 |
| insufficient balance (10195) | 余额不足 | 在 api.newportai.com 充值 |

## Known Limitations

- 短视频（~20s）经常失败，1 分钟以上的视频稳定
- 处理时间约 5 分钟 / 3.5 分钟视频，大致线性
- API 返回的视频 URL 会过期，需及时下载（脚本自动处理）
