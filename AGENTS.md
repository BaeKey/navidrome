# Navidrome Local Patch Guide

本文件适用于整个仓库，供维护本地补丁的开发者和代码代理使用。先读本文件，再修改代码。

## 仓库模型

- `master` 必须指向选定的 Navidrome 上游提交或发版标签。
- `my-patches` 必须始终保持为 `master + 1 个本地提交`。
- 本地提交主题是：`支持设置全局输出格式与码率，CDN分发`。
- 同步上游时使用 rebase，不要把 `master` merge 进 `my-patches`。
- 不要在文档中固化补丁 SHA；rebase 或 amend 后 SHA 会变化。
- 上游自带的 `README.md`、`CONTRIBUTING.md` 和 `CODE_OF_CONDUCT.md` 不属于本地补丁说明。

## 补丁目的

### 统一音频输出

配置项：

- `AudioOutputFormat`：非空时启用全局输出策略，例如 `mp3` 或 `opus`。
- `AudioMaxBitRate`：全局输出码率，单位为 kbps。

启用后，服务端覆盖客户端传入的输出格式和码率，并在最终流请求进入缓存前固定：

```text
Format     = AudioOutputFormat
BitRate    = AudioMaxBitRate
SampleRate = 0
BitDepth   = 0
Channels   = 2
Offset     = 0
```

目标是让同一媒体文件只产生一份规范化转码缓存。`Offset = 0` 是必要约束；完整缓存后的定位交给支持 `Range` 和 `206 Partial Content` 的外层 CDN/COS。

当 `AudioOutputFormat` 为空时，必须保持 Navidrome 上游行为。

### 统一图片输出

配置项：

- `ArtworkSize`：大于 0 时覆盖客户端传入的图片尺寸；为 0 时保持上游行为。
- `ArtworkSquare`：全局图片输出是否使用正方形画布；仅在 `ArtworkSize` 大于 0 时生效。

启用后，统一 artwork 服务、图片 URL 生成和预缓存都使用配置中的尺寸与形状，避免同一图片因客户端传入不同 `size`/`square` 而生成多份缩放缓存。

图片缓存仍按源图片内容 hash、统一输出尺寸、形状和编码质量复用；不同源图片（例如启用媒体文件封面后每首歌的不同嵌入图片）不会强行合并。

当 `ArtworkSize` 为 0 时，必须保持 Navidrome 上游图片请求、URL 和预缓存行为。

### CDN/COS 分发完整缓存

配置项：

- `CacheAccelRedirectPrefix`：Nginx 内部跳转路径前缀；为空时禁用。

完整命中的音频或图片缓存返回 `X-Accel-Redirect`，典型路径为：

```text
/<prefix>/transcoding/xx/yy/<sha1>
/<prefix>/images/xx/yy/<sha1>
```

Navidrome 只决定内部路径。Nginx、CDN 和 COS 的路径映射、鉴权、缓存策略及 Range 支持由部署环境负责。

缓存文件只有同时满足以下条件时才能重定向：

1. `getFinalCachedSize` 确认底层缓存已写入终态。
2. 同目录存在 `<sha1>.complete` 标记。

标记后缀是 `.complete`，不是 `.ok`。首次转码或仍在写入的缓存继续由 Navidrome 流式返回，不能交给 CDN。

## 不可破坏的契约

- `MakeDecision` 只根据显式传入的 `ClientInfo` 决策，不从 request context 读取全局输出策略。
- `NewStream` 必须在生成转码 cache key 前调用 `ApplyAudioOutputToRequest`。
- Legacy Subsonic、token、归档/下载/分享和 OpenSubsonic 协商入口都必须应用全局输出策略。
- 只有 `Cached && Complete` 且相对路径非空时，`AccelRedirect` 才能成功。
- `ArtworkSize > 0` 时，统一 artwork 服务必须覆盖客户端的 `size` 和 `square`；图片 URL 生成和预缓存必须使用相同的有效规格。
- 图片缓存文件没有扩展名；重定向响应必须保留正确的 `Content-Type`，包括 WebP 检测。
- `mapCacheKeyToPath` 的 SHA-1 两级分散路径必须稳定，否则 CDN/COS 映射会失效。
- 正常写入路径只能在缓存写入及关闭成功后创建 `.complete`；保留 `Reload` 首次升级时迁移旧缓存的上游逻辑。删除缓存时也要删除对应标记。
- 同时启用全局音频输出和 accel redirect 时，首个客户端断开后 ffmpeg 仍需完成规范缓存。修改这段 context/limiter 行为前必须评估资源占用和缓存完整性。
- 配置未启用时不得改变上游的直出、转码、取消和缓存行为。

## 代码地图

| 范围 | 主要文件 | 维护重点 |
| --- | --- | --- |
| 配置 | `conf/configuration.go` | 音频、图片和 accel redirect 配置项及默认值 |
| 输出策略 | `core/stream/output_policy.go` | 格式、码率和请求归一化 |
| 转码与缓存键 | `core/stream/media_streamer.go` | `NewStream` 顺序、后台转码 context、重定向接口 |
| Legacy/token | `core/stream/legacy_client.go`, `core/stream/token.go` | 决策前应用服务端策略 |
| 下载与分享 | `core/archiver.go` | 归档输出遵循全局策略 |
| Subsonic/OpenSubsonic | `server/subsonic/stream.go`, `server/subsonic/transcode.go` | 协商、stream、download、transcode stream |
| 公共音频 | `server/public/handle_streams.go` | 分享流和重定向响应头 |
| 图片 | `core/artwork/artwork.go`, `core/artwork/worker.go`, `core/publicurl/publicurl.go`, `server/public/handle_images.go`, `server/subsonic/media_retrieval.go` | 全局规格归一化、预缓存、Content-Type、缓存头和重定向 |
| 缓存 | `utils/cache/file_caches.go`, `utils/cache/spread_fs.go` | 完整性标记、相对路径、哈希映射 |
| 自定义镜像 | `.github/workflows/custom-docker-publish.yml` | 手动构建并推送 ghcr.io 镜像 |
| 上游 CI | `.github/workflows/pipeline.yml` | fork 中仅保留 PR 触发 |

## 同步上游

目标提交记为 `<target>`。开始前必须确认工作区干净，且目标确实属于 `upstream` 历史。

```bash
git status --short
git fetch upstream
git show -s --oneline <target>
git merge-base --is-ancestor master <target>
git merge-base --is-ancestor <target> upstream/master
```

保持在 `my-patches` 上，将未检出的 `master` 快进到目标，再 rebase 唯一补丁：

```bash
git branch -f master <target>
git rebase master
```

发生冲突时，先理解上游的新契约，再按本文件的不可破坏契约恢复本地能力。不要机械采用某一侧，也不要创建 merge commit。

rebase 后检查：

```bash
git rev-list --count master..HEAD
git log --oneline --decorate --graph -10 master HEAD
git diff --stat master..HEAD
git diff --check master..HEAD
```

`git rev-list` 的结果必须是 `1`。同时比较 `<old-master>..<target>` 与 `master..HEAD` 的文件列表，重点审查双方都修改过的文件。

## 验证

修改 Go 文件后先格式化实际改动的文件，然后运行补丁相关测试：

```bash
gofmt -w <changed-go-files>
GOCACHE=/tmp/navidrome-gocache go test ./core/stream ./server/subsonic ./server/public ./utils/cache
git diff --check master..HEAD
```

如果 `utils/cache` 仅因沙箱禁止 `httptest` 绑定本地端口而失败，应在允许本地监听的环境中单独重跑：

```bash
GOCACHE=/tmp/navidrome-gocache go test ./utils/cache
```

不能把环境限制导致的监听失败描述成代码失败。任何真实测试失败都必须在推送前解决。

## 提交与推送

- 本地补丁内容变化时 amend 当前提交，不要新增第二个提交。
- rebase 或 amend 后先再次确认 `master..HEAD` 只有一个提交。
- 远端历史需要更新时使用：

```bash
git push --force-with-lease origin my-patches
```

- 未经明确要求不要推送，也不要使用不带 lease 的强制推送。
