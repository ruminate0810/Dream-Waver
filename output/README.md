# output/ — skill 产物统一落点

仓库自带一套创意生产 skills(见 `.claude/skills/`,共 25 个)。**所有生成的媒体文件
一律落在这个目录**,按「领域/任务 slug」分层,git 永远不追踪(只追踪本 README)。

## 目录约定

```
output/
├── films/<slug>/            # cinematic 全家桶(OpenMontage)
│   ├── story_spec.json      #   分镜 spec(cinematic-spec 产出)
│   ├── chars/               #   角色三视图(cinematic-chars)
│   ├── frames/              #   每镜首帧(cinematic-frames)
│   ├── clips/               #   每镜视频片段(cinematic-clips)
│   └── final.mp4            #   成片(cinematic-compose)
├── images/<slug>/           # 抠图/高清化/上色/改尺寸/网站风格图 的输入与产出
├── posters/<slug>/          # poster-generator / business-card / amazon 产品图
├── storybooks/<slug>/       # storybook-generator + interactive-storybook 的
│                            #   插画 PNG + manifest + 交互绘本 HTML
├── videos/<slug>/           # 文章转视频 / 视频翻译 / 字幕 / 下载 的成品
└── slides/<slug>/           # frontend-slides / dreamface-video 的 HTML 与 MP4
```

## 规则

1. **slug 用短横线小写**(`pug-farm-morning`),一个任务一个目录,可断点续跑
   (cinematic 系列本身就是 skip-if-exists 的)。
2. **不进 git**:`.gitignore` 里 `output/*` 兜底;要长期保存就挪到云盘/OSS。
3. **密钥一律走环境变量**(`NEWPORT_AI_API_KEY` / `DF_ABILITY_*` / `GEMINI_*`,
   在仓库根 `.env` 里配置)——仓库里的 skills 副本已洗掉所有内置兜底 key。
4. 产品侧(Claw 作品包)的产物是 OSS/数据库托管的,与本目录无关;这里只管
   Claude Code 会话里 skills 跑出来的本地文件。
