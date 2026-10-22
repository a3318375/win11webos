# 云盘挂载 API

Go + Gin + 纯 Go SQLite。当前支持本地目录真实挂载、115 / 百度 Token 与夸克 Cookie 的文件列表读取、夸克扫码，以及可配置的 OAuth 服务对接。前端云盘应用已使用后端账号和文件接口，旧 localStorage 演示账号不会自动导入。

## 本地运行

在项目根目录的 PowerShell 中运行：

```powershell
$env:CLOUD_LOCAL_ROOTS = (Resolve-Path ./public).Path
cd api
go mod download
go run ./cmd/server
```

另开终端在项目根目录运行 `npm run dev`。访问 `http://localhost:5173`，打开桌面“云盘”→“关联网盘”。选择本地 NAS，填写名称，点击“浏览”选择允许范围内的实际目录，测试连接并添加挂载。

默认后端 `127.0.0.1:8081`，Vite `/api` 代理到这个地址。未实现管理员登录前，API 强制绑定回环地址，Vite 拒绝来自非本机连接的 `/api` 请求；当前不提供公网或局域网共享访问。外部 Origin 的写请求也会被拒绝。

## 配置

配置读取环境变量，不自动读取 `.env`：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `CLOUD_API_ADDR` | `127.0.0.1:8081` | 必须是回环监听地址，修改后同步 Vite 代理 |
| `CLOUD_DATA_DIR` | `data`（相对启动目录） | SQLite 与加密密钥所在目录 |
| `CLOUD_LOCAL_ROOTS` | `data/mounts` | 允许挂载的实际目录范围；Windows 多目录用 `;`，Linux 用 `:` 分隔 |
| `CLOUD_OAUTH_URL` | 未配置 | 可信、兼容的 HTTPS OAuth 服务；不内置第三方服务地址 |

本地驱动浏览文件时使用 Go `os.Root` 限制访问范围，拒绝路径穿越，目录列表跳过符号链接。可挂载范围取决于后端启动参数，重新启动时需保留相同允许目录。本次开发验证使用项目 `public` 目录，不访问用户私人目录。

数据目录包含 `cloud.db` 和 `credentials.key`。长期 Token / Cookie 经 AES-256-GCM 加密，与账号元数据同一记录保存。备份时同时备份数据库和密钥；丢失密钥后已有凭据无法解密，服务不会自动生成新密钥覆盖这一情况。文件权限依赖运行设备的文件系统和账户权限。

## 真实网盘接入

- **115**：手动填写访问 Token / 刷新 Token，或配置 OAuth 服务后点击“自动获取 Token”。驱动请求根目录列表验证权限；凭据过期时尝试刷新，轮换的新 Token 立即加密保存。
- **百度**：手动填写访问 Token，或使用刷新 Token 配合 OAuth 服务；自动获取 Token 也依赖该服务。目录必须在对应应用授权范围内，默认 `/` 不保证对所有应用可读。
- **夸克**：点击扫码按钮，由后端请求二维码并在本机生成图片。使用夸克 App 扫码确认后，后端取得 Cookie；也支持手动 Cookie。Cookie 失效后需要重新授权。
- **本地 NAS**：选择后端能访问的目录。如果是远端 SMB / NFS 共享，需要先挂载到运行设备。本版不负责远端 SMB / NFS 协议登录。

自动授权会话保存在后端内存中，约 5 分钟过期。前端只接收随机会话 ID、状态、授权地址或二维码，不接收长期凭据。点击测试连接或添加挂载时，后端使用会话内凭据。关闭弹窗取消会话；服务重启后需重新发起未完成授权。

`CLOUD_OAUTH_URL` 需由部署者提供并信任，兼容接口为：

```text
POST /api/oauth/start
  输入 driver_type（115网盘Open / 百度网盘Open）、server_use、callback_url
  响应 {success:true,data:{session_id,oauth_url}}
GET /api/oauth/status/{session_id}
  响应 {success:true,data:{status:pending|success|error,token_data:{access_token,refresh_token}}}
POST /api/oauth/confirm-received/{session_id}
POST /api/oauth/refresh
  输入 driver_type、refresh_token
  响应 {success:true,data:{access_token,refresh_token}}
```

这里实现的是 OAuth 服务客户端，不包含网盘开放平台应用申请、第三方回调服务或应用密钥。未配置时前端明确禁用 115 / 百度自动授权按钮，可继续手动填写。没有真实网盘账号授权时，只能完成协议模拟测试，不能保证当前账号权限、会员限制或上游变更后的兼容性。

## 目录缓存与浏览

- API 以账号 ID + 目录 ID 浏览，前端维护面包屑。远程目录从挂载根目录逐级发现，避免直接请求未验证的其他目录 ID。
- 账号缓存为 0 时读取上游；大于 0 时优先缓存，返回 `cacheHit` 和 `fetchedAt`。刷新按钮显式绕过缓存。
- 空目录同样缓存，同目录并发使用 singleflight 合并；远程请求按账号串行，保护刷新 Token 轮换。
- 缓存限制 128 个目录、估算 32 MiB 的条目预算，超出时淘汰旧目录；这是估算软预算，不等于进程内存硬上限。
- 当前远程驱动先汇总上游分页，再返回完整目录，最多读取 200 页。本版尚未实现上游首屏增量分页；超大目录应挂载更小的子目录。
- 前端每次最多追加显示 100 项，搜索仅筛选已获取的当前目录；不是递归搜索或窗口化虚拟列表。
- 本地图片 / 文本支持预览，普通文件支持下载与 HTTP Range。夸克支持服务端代理下载，Cookie 不返回浏览器。
- 夸克与本地 NAS 支持新建目录、重命名、批量复制 / 移动 / 移入回收站，每批最多 100 项，操作后清除账号目录缓存。夸克复制轮询异步任务确认完成；远程写操作经过模拟协议测试，真实账号兼容性仍需联调。
- 同名项目会拒绝覆盖，暂不支持跨挂载粘贴。本地删除将文件移至根目录的 `.cloud-trash`，可用系统文件管理器找回；夸克使用网盘回收站。115 / 百度暂不提供文件写操作，上传与离线下载暂未接入。
- 收藏只在本浏览器保存文件定位信息，最近使用仅记录本次窗口打开的文件。解除挂载删除配置和凭据，不删除原文件。

## 接口

### 后台下载到本地

- `POST /api/v1/transfers`：`{sourceAccount,sourceParent,sourceTrail,ids,targetAccount,targetParent,conflict}`，`conflict` 为 `skip` 或 `rename`。返回 202 和任务，任务由后台执行。
- `GET /api/v1/transfers`：任务列表，含状态、当前文件、已处理/总字节、完成/跳过文件数、速度和错误。
- `POST /api/v1/transfers/:id/action`：`{action:"pause"|"cancel"|"retry"|"remove"}`。`retry` 用于继续暂停或重试失败/取消任务；`remove` 仅移除已停止的记录。
- 来源支持夸克、本地 NAS，目标必须是本地 NAS；每批最多选择 100 项，每任务最多扫描 20000 项、128 层。后台最多并行 2 个任务，保留最多 500 条记录。
- 关闭前端窗口不停止下载；服务重启后恢复为暂停。继续时保留已完成文件，当前文件重新下载，不提供字节级断点续传。
- SQLite 任务 JSON 不保存 Token/Cookie；目标写入受 `os.Root` 和允许挂载范围约束。临时文件位于目标卷，完整写入后才发布，失败/取消清理当前临时文件。
- 对照与验证见 [本地传输设计](../docs/local-transfer-design.md)。

成功响应 `{data:...}`，错误 `{error:{code,message}}`。账号与列表响应均不含凭据。

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | `/api/health` | 健康检查 |
| GET | `/api/v1/providers` | 驱动、配置字段、授权可用性和能力 |
| GET / POST | `/api/v1/accounts` | 挂载列表 / 验证并创建挂载 |
| POST | `/api/v1/accounts/test` | 测试凭据及根目录；返回成功状态和短期凭据引用 |
| DELETE | `/api/v1/accounts/:id` | 解除挂载 |
| GET | `/api/v1/accounts/:id/files?parent=&refresh=false` | 目录列表与缓存状态 |
| GET | `/api/v1/accounts/:id/content?id=&parent=&download=false` | 本地预览 / 下载、夸克代理下载，支持 Range |
| POST | `/api/v1/accounts/:id/actions` | `{action:mkdir/rename/copy/move/trash,parent,sourceParent,ids,name}`；错误可包含已完成数量 `completed` |
| GET | `/api/v1/local/directories?path=` | 允许范围内的服务端目录选择 |
| POST | `/api/v1/auth/sessions` | 创建授权会话 |
| GET / DELETE | `/api/v1/auth/sessions/:id` | 轮询 / 取消授权 |

## 代码与验证

```text
cmd/server/          启动、配置装配、优雅退出
internal/api/        Gin 路由与 HTTP 边界
internal/config/     环境变量与本机部署约束
internal/mount/      挂载编排、SQLite、加密、缓存、本地/远端读取、授权会话
internal/storage/    配置字段与通用文件契约
```

在 `api/` 中运行：

```sh
go test ./...
go vet ./...
go build -o bin/cloud-drive-api.exe ./cmd/server
```

Linux 构建可去掉 `.exe`。测试覆盖真实临时目录、SQLite 重开、加密与 DTO、缓存命中和强制刷新、请求合并、路径越界、Range、Origin、模拟 Token 刷新、模拟扫码及授权会话失效。真实网盘需用户授权后继续做账号级联调。
