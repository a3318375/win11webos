# LitePan / OpenList 文件浏览源码对比

研究日期：2026-10-22。静态阅读官方仓库，不登录网盘、不运行性能测试，不复制上游实现。

- LitePan：`e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b`，2026-10-02。
- OpenList：`70ed5eedb8edeaee67260a5b83efa22a33749257`，2026-10-22。
- 具体驱动比较范围：115 Open、百度、夸克 Cookie；不把夸克 Open / TV 等不同驱动混为同一实现。
- LitePan 阅读了前端与后端；OpenList 本次以主仓库后端为主，未对其独立前端仓库作完整审查。

## 1. 调用链

LitePan：

```text
browser store 选择账号、维护面包屑和当前 parentID
  → GET /api/files/list?account_id=...&parent_id=...&force_refresh=...
  → API listFiles
  → file.Service.List
  → 认证状态检查、目录缓存、同目录并发合并
  → driverexec.Executor.Run
  → 认证刷新重试、网络退避、获取账号驱动实例
  → driver.ListFiles(parentID)
  → 网盘分页请求汇总为 []FileItem
  → 返回当前目录全部项目
  → 前端排序、列表/网格展示
```

OpenList：

```text
客户端请求 /api/fs/list，传 path、password、page、per_page、refresh
  → FsListSplit / FsList
  → 用户基础路径、目录元数据、密码与权限校验
  → fs.List
  → 找到挂载存储、解析存储内路径
  → op.List
  → 目录缓存、同目录并发合并、解析目录对象
  → storage.List(dirObj,args)
  → 网盘分页请求汇总为 []Obj
  → 合并子挂载产生的虚拟目录、隐藏及逐项读取权限过滤
  → pagination 对完整结果切片
  → 返回 content、total、缩略图、签名、读写信息等
```

源码：[LitePan API](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/internal/api/files.go)、[LitePan 文件服务](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/internal/file/service.go)、[OpenList API](https://github.com/OpenListTeam/OpenList/blob/70ed5eedb8edeaee67260a5b83efa22a33749257/server/handles/fsread.go)、[OpenList fs 层](https://github.com/OpenListTeam/OpenList/blob/70ed5eedb8edeaee67260a5b83efa22a33749257/internal/fs/list.go)、[OpenList op 层](https://github.com/OpenListTeam/OpenList/blob/70ed5eedb8edeaee67260a5b83efa22a33749257/internal/op/fs.go)。

## 2. 核心模型差异

| 方面 | LitePan | OpenList |
| --- | --- | --- |
| 浏览入口 | 账号 ID + 父目录 ID | 统一挂载路径 |
| 文件身份 | 通用 FileItem，包含 IDKind；各驱动可用稳定 ID 或路径 | Obj 接口保留 ID/Path 等，公共浏览 API 主要通过路径定位 |
| 面包屑 | 前端保存目录 ID 和名称序列 | 服务端将统一路径解析到挂载存储，再定位对象 |
| 跨盘展示 | 本次主浏览器以账号切换为单位 | 文件列表可混合实际文件与子挂载产生的虚拟目录 |
| 目录访问控制 | 主列表路由区分公开浏览或管理员，账号及认证状态统一检查 | 用户基础路径、目录密码、元数据继承、隐藏规则和逐项读取权限 |
| 列表响应 | parent_id + items，文件信息较精简 | content + total + readme/header/write 等页面与权限信息，项目含 thumb/sign |

不能将 LitePan 概括为“只支持 ID”：百度和本机驱动使用路径；也不能将 OpenList 概括为“驱动里没有文件 ID”：115、夸克仍使用原始 ID 调上游。差别主要在公共 API 和命名空间。

按账号和不透明 ID 访问更贴合本项目左侧网盘切换的界面；统一路径更适合跨盘挂载、分享链接和目录权限。稳定 ID 可以降低名称变动导致定位失效的概率，但移动后的面包屑仍需要更新；路径模型也可借助映射优化，不能直接等同于性能差。

## 3. 分页：页面分页不等于网盘分页

两边的上述驱动都在 List 内循环请求上游多页，累计后返回。

- LitePan 列表 API 没有 page / cursor 参数，返回整目录。
- OpenList 的 page / per_page 在 `FsList` 调完 `fs.List` 后才由 `pagination` 切片，因此通常仍需先完成该目录的上游读取。
- OpenList 可以减少发往浏览器的数据量，但这不保证首次读取上游的请求数更少。
- LitePan 列表视图用 `files.slice(0,listVisibleCount)` 和 IntersectionObserver 逐批增加渲染项目，属于前端渐进渲染；完整列表已在内存中。不是上游增量分页，也不是滚出视口就卸载的窗口化虚拟列表。网格分支直接遍历 files。

源码：[OpenList 分页](https://github.com/OpenListTeam/OpenList/blob/70ed5eedb8edeaee67260a5b83efa22a33749257/server/handles/fsread.go#L214)、[LitePan 列表渲染](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/web/src/components/file/FileTable.vue)、[OpenList 115 驱动](https://github.com/OpenListTeam/OpenList/blob/70ed5eedb8edeaee67260a5b83efa22a33749257/drivers/115_open/driver.go)、[OpenList 百度分页](https://github.com/OpenListTeam/OpenList/blob/70ed5eedb8edeaee67260a5b83efa22a33749257/drivers/baidu_netdisk/util.go)、[OpenList 夸克分页](https://github.com/OpenListTeam/OpenList/blob/70ed5eedb8edeaee67260a5b83efa22a33749257/drivers/quark_uc/util.go)。

## 4. 缓存与强制刷新

共同点：都使用目录缓存、TTL 和 singleflight 类的并发合并，避免同一目录多人同时访问时重复拉取。都提供显式刷新参数。普通访问不保证立即看到网盘外部发生的改动。

LitePan：

- 目录键围绕账号和 parentID 构造。
- 元数据缓存有 LRU、字节软上限、TTL；可配置 JSON 快照持久化。
- 目录缓存开关优先于账号 cache_ttl，其次是全局默认；账号 0 表示该目录读取绕过缓存。
- 空目录结果也缓存。
- 强制刷新先失效对应目录键再获取。
- 文件变更通过 FileMutated 事件处理：创建可原地更新缓存，删除/重命名/移动/复制失效相关目录、文件信息及下载链接。
- 写后对相关目录设置约 3 秒冷却，期间读上游且不回写常规目录缓存，避免上游最终一致性把旧结果长期缓存。

OpenList：

- 常规目录缓存键来自挂载路径和存储内路径。
- TTL 来自存储 CacheExpiration，可通过 glob 路径策略覆盖；驱动可声明 NoCache。
- 所读 `op.list` 对空目录清理目录树缓存，不保存常规空列表缓存。
- refresh 跳过目录缓存读取，加载后更新缓存；强制刷新还受写入相关权限限制。
- 缓存管理器支持修改目录对象、删除目录树、链接失效、脏标记重排序；后续读取可复用维护后的结果。

这些是实现差异，不构成实测速度或内存占用结论。没有确认 OpenList 所有缓存子系统的持久化行为，因此不作“OpenList 完全不支持持久化”的判断。

源码：[LitePan 缓存](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/internal/cache/service.go)、[LitePan 写后失效](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/internal/cache/cleaner.go)、[冷却机制](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/internal/cache/fence.go)、[OpenList 缓存](https://github.com/OpenListTeam/OpenList/blob/70ed5eedb8edeaee67260a5b83efa22a33749257/internal/op/cache.go)。

## 5. 稳定性与扩展

LitePan 把文件域的账号认证检查、被动刷新重试、网络故障退避集中到 driverexec.Executor。其当前代码连续 3 次网络失败后进入约 30 秒退避，并会丢弃失效传输实例，下次重新构造。驱动管理器按账号复用实例，静态配置改变时重建。前端使用请求序号和账号/目录匹配丢弃过时响应，目录进入失败可以回滚面包屑。

OpenList 的公共 fs/op 层承担挂载路径、对象查找、权限、缓存、排序、对象更新 Hook 等通用能力；具体网盘的认证与请求策略还需逐驱动阅读。两者都以接口和可选能力扩展操作，不能简单断言只有一边支持模块化驱动。

文件浏览与下载解析在两者中都是分开的操作，不需要列目录就给每个文件请求下载直链。我们应沿用这一边界，预览/下载点击后再解析链接。

源码：[LitePan 执行器](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/internal/core/driverexec/exec.go)、[LitePan 前端导航](https://github.com/Ponphil/LitePan/blob/e0e29c0e4c7fb4bc172d9bb66cb0292911b6693b/web/src/stores/browser.ts)、[OpenList 驱动契约](https://github.com/OpenListTeam/OpenList/blob/70ed5eedb8edeaee67260a5b83efa22a33749257/internal/driver/driver.go)。

## 6. 本项目建议

以下是基于阅读的设计建议，未实现：

1. 浏览 API 保持 accountID + 不透明目录 ID；名称路径用于显示，给将来的 WebDAV 另设路径解析层。
2. 保留适配器分页契约，为支持增量分页的上游先返回一页，降低大目录首屏等待；只有整目录读取能力的驱动明确声明限制。
3. 分页与全局排序一起设计：不能对每页分别排序后宣称整个目录已排序。采用上游排序或完整目录快照后再稳定分页，并约束快照失效。
4. 列表返回 cacheHit、fetchedAt、hasMore 等状态，刷新保留旧内容；过时响应丢弃，失败时给重试入口。
5. 缓存按账号/目录/查询条件隔离；同时做 TTL、内存容量限制、并发合并、写后失效和负缓存，首版可不做磁盘快照。
6. 上游读取、预览解析、缩略图分别限流，目录列表不预取全部文件直链。
7. 使用虚拟列表限制 DOM 数量；前端窗口化、API 分页、上游分页分别验证，避免把其中一个当作全部性能优化。
8. 先实现账号级读写权限与 NAS 根目录边界；分享和统一挂载需求明确后再增加 OpenList 式完整目录权限体系。

对当前产品的取舍：采用 LitePan 较直接的账号浏览流程，借鉴 OpenList 的权限边界与通用文件操作分层，同时把两边常见的整目录首屏等待作为我们需要单独解决的问题。
