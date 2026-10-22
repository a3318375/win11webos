# 云盘 API

Go + Gin 独立模块，与根目录的 React / Vite 前端并列。当前是可运行骨架，不连接真实网盘，不接收或保存凭据；前端仍使用原来的本地演示数据。

## 开发运行

在项目根目录执行：

```sh
cd api
go mod download
go run ./cmd/server
```

默认监听 `127.0.0.1:8081`。启动时输出 JSON 日志；Ctrl+C 会等待正在执行的请求退出，最多等待 10 秒。

另开终端，在项目根目录执行 `npm run dev`。Vite 会把 `/api` 转发至 `http://127.0.0.1:8081`，无需开发跨域配置。

配置通过环境变量读取，不自动加载 `.env` 文件：

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `CLOUD_API_ADDR` | `127.0.0.1:8081` | HTTP 监听地址 |

修改监听地址后，需要相应修改根目录 `vite.config.ts` 中的代理目标。当前尚无登录鉴权，默认只监听本机。

## 现有接口

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | `/api/health` | 进程健康状态，200 |
| GET | `/api/v1/providers` | 115、百度、夸克、NAS 的字段、必填项、默认值，200 |
| POST | `/api/v1/accounts` | 预留入口，明确返回 501；不读取凭据 |

成功响应为 `{"data": ...}`；错误响应为 `{"error":{"code":"...","message":"..."}}`。不存在的路由和不支持的方法分别返回 JSON 404 / 405。所有存储类型的 `implemented` 当前均为 `false`，配置描述不代表真实接入能力。

## 结构与后续边界

```text
api/
├─ cmd/server/          启动、HTTP 超时、信号与优雅退出
├─ internal/
│  ├─ api/             Gin 路由、响应与访问日志
│  ├─ config/          环境变量配置
│  └─ storage/         文件模型、适配器接口、网盘配置描述
├─ go.mod
└─ go.sum
```

`storage.Adapter` 定义最小只读能力：目录分页、文件元数据和能力声明，所有 I/O 接受 `context.Context`。接入真实网盘后，再为上传、搜索、回收站增加可选接口。

下一阶段逐步添加以下实际模块，不预先堆空目录：

1. `repository` + SQLite 迁移：账号、收藏和任务持久化。
2. `credential`：加密存储、刷新与授权会话；配合登录鉴权后开放账号写入接口。
3. `service`：统一账号与文件业务、存储适配器注册。
4. `storage/local`：从本地 NAS 开始实现，校验路径穿越与符号链接边界。
5. `transfer`：下载地址解析、Range 流式代理、超时及取消；远端凭据不暴露给前端。

不把文件内容存入 SQLite。目录缓存首版使用内存；暂不引入 Redis、ORM 或任务队列服务。日志不输出请求体、Cookie、授权头和查询参数。后续生产部署再加入前端资源嵌入、Docker 和凭据加密密钥配置。

## 验证与构建

在 `api/` 下执行：

```sh
go test ./...
go vet ./...
go build -o bin/cloud-drive-api ./cmd/server
```

Windows 可指定输出 `bin/cloud-drive-api.exe`。构建目录 `bin/` 和未来的数据目录 `data/` 已加入 Git 忽略规则。
