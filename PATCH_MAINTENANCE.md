# Navidrome Local Patch Maintenance

本文记录本仓库本地补丁的背景、目标和后续同步上游时的维护流程。

## 背景

本仓库基于 Navidrome 上游代码维护一个本地补丁分支：

- `master`：保持为选定的上游发版提交或上游目标提交。
- `my-patches`：保持为 `master + 1 个本地补丁提交`。
- 本地补丁提交名：`支持设置全局输出格式与码率，CDN分发`。

核心目标是每次上游发版后，先同步 `master` 到上游发版提交，再把本地补丁重新贴到最新上游代码上。补丁应尽量像一个小而稳定的 patch，方便长期 rebase。

## 补丁目标

本地补丁只负责两类能力。

1. 服务端统一决定音频输出格式和码率

   配置项保持当前命名：

   - `AudioOutputFormat`
   - `AudioMaxBitRate`

   一旦配置了 `AudioOutputFormat`，服务端不接受客户端传入的输出格式、码率、offset、sampleRate、bitDepth、channels 等会导致缓存分裂的参数。最终转码请求统一归一化为服务端配置，目标是同一首歌只生成一份固定转码缓存。

2. 缓存命中时交给 CDN/COS 分发

   配置项：

   - `CacheAccelRedirectPrefix`

   当音频或图片缓存已经完整命中时，Navidrome 返回 `X-Accel-Redirect`，路径形如：

   ```text
   /<CacheAccelRedirectPrefix>/transcoding/xx/yy/hash
   /<CacheAccelRedirectPrefix>/images/xx/yy/hash
   ```

   外层 Nginx/CDN 根据这个路径映射到 COS 中的缓存对象，由 CDN/COS 处理实际读取、缓存和 Range 请求。

## 关键设计约束

- `MakeDecision` 不读取 request context，不在里面处理服务端全局策略。
- `MakeDecision` 保持主线契约：只根据显式传入的 `ClientInfo` 做转码决策。
- 服务端全局输出策略放在边界层和 `NewStream` 入口处理，避免深入修改主线核心决策逻辑。
- 缓存 key 必须稳定，不能因为客户端传入 offset 或音频参数生成多份文件。
- 图片缓存文件是哈希名，没有扩展名；走 CDN 内部跳转时必须带上 `Content-Type`，否则 WebP/JPEG/PNG 可能无法被 CDN 或浏览器正确识别。
- 不把 GitHub Actions 改动作为核心审查重点；它不是本地补丁长期维护的关键逻辑。

## 当前实现要点

音频策略集中在：

```text
core/stream/output_policy.go
core/stream/media_streamer.go
server/subsonic/transcode.go
```

重点行为：

- `ApplyAudioOutput`：旧式 stream/download/share/token 入口使用，配置全局输出后替换格式和码率。
- `ApplyAudioOutputToRequest`：在 `NewStream` 入口统一归一化最终请求。
- `ApplyAudioOutputToClientInfo`：OpenSubsonic `getTranscodeDecision` 入口使用，把客户端能力替换成服务端允许的唯一输出格式。
- `NewStream` 会在真正生成 cache key 前调用 `ApplyAudioOutputToRequest`。

图片和 CDN 响应头集中在：

```text
server/public/handle_images.go
server/subsonic/media_retrieval.go
```

缓存路径能力集中在：

```text
utils/cache/file_caches.go
utils/cache/spread_fs.go
core/stream/media_streamer.go
```

## Offset 与 Range 的关系

Navidrome 原默认策略会把 `offset` 放进转码 cache key，并传给 ffmpeg：

```text
offset=0  -> 一份缓存
offset=30 -> 另一份缓存
offset=60 -> 又一份缓存
```

这不符合“同一首歌只生成一份固定转码缓存”的目标。

本地补丁在全局输出策略启用时把 `Offset` 归零。第一次没有缓存时会从 0 秒开始生成完整转码文件；已有完整缓存后，CDN/COS 通过 HTTP Range 返回某个字节范围。

CDN Range 不是读取“offset 后的新文件”，而是读取同一个完整缓存对象的字节区间：

```http
Range: bytes=1234567-
```

因此需要确认外层 CDN/COS 支持 `Range` 和 `206 Partial Content`。

## 每次同步上游发版的推荐流程

假设上游远程名为 `upstream`，发版提交为 `<upstream-release-commit>`。

1. 获取上游更新：

   ```bash
   git fetch upstream
   ```

2. 重置本地 `master` 到上游发版提交：

   ```bash
   git checkout master
   git reset --hard <upstream-release-commit>
   ```

3. 把 `my-patches` 这一个补丁提交 rebase 到新的 `master`：

   ```bash
   git checkout my-patches
   git rebase master
   ```

4. 如果没有冲突，直接进入测试。

5. 如果有冲突，解决冲突后继续：

   ```bash
   git add <conflicted-files>
   git rebase --continue
   ```

6. 确认 `my-patches` 仍然只有一个本地补丁提交：

   ```bash
   git rev-list --count master..HEAD
   ```

   期望输出：

   ```text
   1
   ```

## rerere

本仓库已启用：

```bash
git config rerere.enabled true
```

`rerere` 会记录已解决过的冲突。以后上游类似改动再次冲突时，Git 可能自动复用之前的解决结果。

检查方式：

```bash
git config --get rerere.enabled
```

期望输出：

```text
true
```

## 验证命令

基础验证：

```bash
go test ./core/stream ./server/subsonic ./server/public ./utils/cache
```

如果本机或沙箱不允许写默认 Go build cache，可以临时指定仓库外或临时目录作为 `GOCACHE`：

```bash
GOCACHE=/tmp/navidrome-gocache go test ./core/stream ./server/subsonic ./server/public ./utils/cache
```

如果 `utils/cache` 因为 `httptest` 监听本地端口失败，需要在允许本地监听的环境中单独运行：

```bash
go test ./utils/cache
```

代码格式和空白检查：

```bash
gofmt -w <changed-go-files>
git diff --check
```

## 推送注意事项

由于 `my-patches` 通过 rebase/amend 保持为 `master + 1 个提交`，提交 SHA 会变化。推送到远程补丁分支时使用：

```bash
git push --force-with-lease origin my-patches
```

不要使用普通 merge 把上游合并进 `my-patches`，否则会产生合并提交，破坏“上游发版提交 + 一个本地补丁”的结构。

## 合并主线时优先检查的风险点

- `core/stream/decider.go`：不要重新把 request context 读取逻辑放进 `MakeDecision`。
- `core/stream/media_streamer.go`：确认 `NewStream` 仍在生成 cache key 前归一化全局输出请求。
- `core/stream/output_policy.go`：确认配置项语义不变，服务端配置优先于客户端传值。
- `server/subsonic/transcode.go`：确认 OpenSubsonic `getTranscodeDecision` 入口仍应用服务端全局输出策略。
- `server/public/handle_images.go` 和 `server/subsonic/media_retrieval.go`：确认图片 CDN 重定向仍带 `Content-Type`、缓存头和 `X-Accel-Redirect`。
- `utils/cache/file_caches.go` 和 `utils/cache/spread_fs.go`：确认完整缓存命中时仍能暴露稳定的内部重定向相对路径。

