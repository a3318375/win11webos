# LitePan 网盘绑定源码研究

研究日期：2026-10-22。研究对象为 [Ponphil/LitePan](https://github.com/Ponphil/LitePan) Go 版，固定提交 `e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b`（2026-10-02）。以下来自静态源码阅读，没有登录用户账号或请求真实授权。旧 Python 版本不在本次范围内。

## 核心结论

“自动获取 Token”是实际的授权流程：前端打开授权页，后端转发到可配置 OAuth 服务，前端轮询授权会话，把返回的 Token 填回当前表单。它不是自动提取浏览器已有登录凭据。115 和百度使用这条路径；夸克通过驱动实现扫码获取 Cookie；本机存储不走授权。

获取凭据与创建账号是两个步骤。授权成功后不会立即新增账号，点击“添加账号”才提交配置，后端通过临时驱动进行 Init + Ping 连接测试，成功后持久化。

## 一、驱动选择与动态表单

- 每个驱动注册自身元信息和配置结构。
- `internal/driver/registry.go` 通过反射解析配置结构的标签，生成字段类型、默认值、必填项、选项和布局信息。
- 通过接口实现情况声明 `supports_oauth`、`supports_qr_login`。
- 前端 `AddAccountDialog.vue` 加载驱动与账号列表，先选择驱动，再渲染 `DynamicForm`。
- 前端检查名称、重名和必填字段；提交包括 `name`、`driver_type`、JSON 字符串 `config`、启用状态等。

源码：[驱动注册](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/internal/driver/registry.go)、[添加弹窗](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/web/src/components/admin/AddAccountDialog.vue)。

## 二、115 / 百度自动获取 Token

实际调用链：

```text
点击自动获取 Token
  → 同步打开空白弹窗，避免异步后被浏览器拦截
  → POST /api/oauth/start，携带 driver_type、server_use
  → LitePan 转发到配置的 OAuth 服务 /api/oauth/start
  → 返回 session_id、oauth_url
  → 弹窗跳转 oauth_url，用户完成登录与授权
  → 前端轮询 GET /api/oauth/status/{session_id}
  → 成功取得 token_data
  → POST /api/oauth/confirm-received/{session_id}
  → 过滤当前驱动接受的字段并回填表单
  → 用户点击添加账号
```

后端会把内部驱动名映射为 OAuth 服务接受的名称，并在没有 callback_url 时补入该服务的 `/callback-popup`。轮询最多尝试 30 次，正常间隔从 2 秒逐步增加至 4 秒，网络错误时退避；115 回填前额外等待 2 秒。关闭弹窗或返回上一步会取消轮询。

源码：[前端授权编排](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/web/src/composables/useOAuthAuth.ts)、[后端 OAuth 转发](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/internal/api/oauth.go)。

重要边界：主仓库里的这部分代码是 OAuth 服务的客户端与转发器，不能仅复制三个 API 就得到完整授权能力。要实现我们自己的入口，还需要明确授权应用、回调、换取凭据及刷新服务的归属。本次没有验证其外部 OAuth 服务当前是否可用，也没有断言外部服务内部采用何种授权交换方式。

## 三、四种存储的差异

| 类型 | 表单凭据 | 根目录 | 下载模式 | 删除模式 | 默认缓存 |
| --- | --- | --- | --- | --- | --- |
| 115 Open | access_token、refresh_token 均标必填 | 目录 ID，默认 0 | 302 重定向 / 本机代理，默认重定向 | 回收站 / 永久删除 | 30 分钟 |
| 百度 Open | refresh_token 必填，access_token 可选 | 路径，默认 / | 重定向 / 代理，默认代理 | 仅回收站 | 30 分钟 |
| 夸克 | Cookie 必填，可扫码回填 | 目录 ID，默认 0 | 仅本机代理 | 回收站 / 永久删除 | 30 分钟 |
| 本机存储 | 无网络凭据，选择 root_path | 服务所在机器或容器内的目录 | 不提供此选项 | 不提供此选项 | 0 分钟 |

表单必填与驱动底层能力要区分：115、百度的 Init 实际允许只有刷新 Token，缺少访问 Token 时先尝试刷新；但 115 表单仍把两个 Token 标成必填。

连接检查也不同：115、夸克请求配置根目录的少量列表；百度请求用户信息，所以百度连接通过不保证配置目录可读；本地驱动检查目录存在、目录类型，并解析符号链接。我们自己的连接测试应额外验证根目录可列举。

源码：[115 配置](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/drivers/115_Open/config.go)、[百度配置](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/drivers/Baidu_Open/config.go)、[夸克配置](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/drivers/Quark/config.go)、[本机驱动](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/drivers/LocalFs/driver.go)。

## 四、夸克扫码获取 Cookie

```text
POST /api/qr/start
  → 构造临时夸克驱动
  → 调用夸克 CAS 取得二维码 Token
  → 返回二维码图片、会话 Token、有效期（300 秒）
  → 前端每约 2 秒 POST /api/qr/poll
  → 用户扫码确认后取得 service_ticket
  → 请求网盘 account/info 换取登录 Cookie
  → 访问首页及根目录列表，补齐实际文件接口所需 Cookie
  → 返回 Cookie 并回填表单
```

驱动轮询状态为 waiting / success / failed / expired。前端处理加载、网络错误和倒计时，过期后可重新获取。源码中的续询 Token 是 base64 编码的会话数据，包含 CAS Token、Cookie 和创建时间；base64 不等于加密。我们自己的实现建议服务端保存短期会话，浏览器仅拿随机会话 ID。

源码：[扫码 API](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/internal/api/qr.go)、[夸克扫码驱动](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/drivers/Quark/qrlogin.go)、[扫码弹窗](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/web/src/components/admin/QrLoginModal.vue)。

## 五、保存账号、连接测试与持续可用性

1. `POST /api/admin/accounts` 转入账号应用服务。
2. 验证账号名称、驱动类型及重名。
3. `OpenEphemeral` 创建临时驱动，注入配置和 OAuth 服务地址。
4. 依次 `Init`、`Ping`，失败时拒绝添加；新增 OAuth 账号的部分可重试错误会再试一次。
5. `SplitConfig` 将静态配置与认证字段分开。
6. 创建账号记录，再写 `account_auth_states`；源码此处是顺序调用，不能据此宣称两步具有事务原子性。
7. 为启用账号注册认证调度；文件浏览时驱动管理器再注入凭据、初始化并复用实例。
8. 列表通过统一文件服务调用 `ListFiles`，配合目录缓存。

115 刷新 Token 直接调用其官方刷新接口；百度刷新调用可配置 OAuth 服务的 `/api/oauth/refresh`。新凭据回写认证状态。夸克不使用 refresh_token：它检查 Cookie，并吸收响应中的部分更新 Cookie；认证彻底失效时仍需重新扫码。账号修改后会丢弃旧驱动、失效相关缓存。

源码：[创建与更新](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/internal/account/service.go)、[连接检查](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/internal/account/ping.go)、[115 刷新](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/drivers/115_Open/auth.go)、[百度刷新](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/drivers/Baidu_Open/auth.go)、[夸克认证维护](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/drivers/Quark/auth.go)。

本机目录浏览由管理员 API `/api/admin/local-fs/browse` 列举服务端目录，和浏览器上传文件的选择框无关。如果 NAS 通过 SMB / NFS 提供文件，需先挂载到服务机器，或另做协议适配器；本机驱动自身不负责登录远程 NAS。

## 六、对本项目的设计建议（尚未实现）

优先呈现自动授权：选择网盘 → 点击授权 / 扫码 → 自动填写账号资料 → 测试连接 → 保存。把手动 Token / Cookie 输入放到备用入口，保留现有简洁 UI。

- 区分授权会话、存储配置、长期凭据三类数据。
- 由后端返回每个网盘的真实表单与能力，限制夸克下载方式、百度删除方式；我们当前演示代码这两项仍需收紧。
- 授权成功后返回短期凭据引用，避免将长期 Token 回传浏览器；手动输入则直接提交后端。
- 连接检查同时验证凭据、根目录权限，输出可理解的失败原因；保存账号与凭据使用事务。
- Token 轮换串行化，更新后立即持久化；区分过期、限流、网络故障，避免所有错误都要求重新登录。
- NAS 目录选择受服务端允许范围约束；数据库保存绝对目录配置，文件 API 使用受控相对路径或不透明 ID。

建议的接口边界：

```text
GET    /api/v1/providers
POST   /api/v1/auth/sessions
GET    /api/v1/auth/sessions/:id
DELETE /api/v1/auth/sessions/:id
POST   /api/v1/accounts/test
POST   /api/v1/accounts
GET    /api/v1/accounts/:id/files
GET    /api/v1/local/directories
```

这是流程与接口研究记录，未复制上游实现，也没有接入其 OAuth 代理。上游仓库声明 PolyForm Noncommercial 1.0.0，后续如考虑复用代码需要单独核对许可；我们当前保持独立实现。
