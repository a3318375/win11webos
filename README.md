# Windows 11 Web Desktop

新会话接手请先看 [项目交接记录 · 2026-10-22](docs/handoff-2026-10-22.md)；协作约定和自动读取入口见 [AGENTS.md](AGENTS.md)。

使用 React 19、Vite、TypeScript 与 CSS 实现的纯前端桌面 Demo。以 Windows 11 23H2 中文浅色桌面为主要 UI 对齐目标，采用原版壁纸和应用图标，包含深色模式。适合桌面浏览器，建议 1280×800 或更大的视口。

## 运行

Go + Gin 挂载后端位于 [`api/`](api/README.md)，默认端口 8081。前端开发服务会代理 `/api` 请求，现有桌面功能仍可独立运行。

```sh
npm install
npm run dev
```

打开终端中显示的地址。生产构建使用 `npm run build`，预览构建使用 `npm run preview`。

## 交互

- 双击桌面图标启动应用；窗口可拖动、调整大小、最小化、最大化和关闭。
- 资源管理器支持文件夹导航、搜索、新建文件夹、剪切、复制、粘贴、重命名、删除与回收站还原。
- 双击文本文件在记事本中编辑，内容自动保存。
- 开始菜单支持应用搜索、固定应用、推荐项目和电源菜单。
- 设置支持界面导航、切换深浅色与壁纸，其他系统选项为演示。
- 任务栏右侧打开快速设置或通知日历；开始菜单的电源按钮提供锁屏入口。
- 锁屏后点击或按 Enter，点击“登录”返回桌面，不需要密码。
- 桌面右键菜单提供刷新、新建文件夹、显示设置和个性化入口。
- Ctrl+E 打开资源管理器；Esc 关闭面板和弹窗。

文件与设置使用 localStorage 保存。虚拟文件不访问操作系统真实文件；网络、声音和电源控件只影响本 Demo。全部运行素材打包在 public 下，不依赖在线图标或壁纸加载。

## UI 范围

目标是复现 Windows 11 的系统界面，而非重新设计。当前覆盖上述主要桌面界面；尚未按特定系统截图进行逐像素差异校准，部分线性图标、响应式布局和次级设置页仍有差异，不能视为全系统的严格 1:1 实现。

## 素材来源

- Windows 壁纸与系统图标：[windows-ui-assets](https://github.com/bartekl1/windows-ui-assets)
- Windows 应用图标：[win11React](https://github.com/blueedgetechno/win11React/tree/master/public/img/icon)
- 线性工具图标：lucide-react（ISC）

Windows、Microsoft Edge 和相关应用图标归各自权利人所有。本项目用于界面学习与演示。

## 文件交互 Demo

- 桌面空白、文件夹空白、文件本身、回收站项目以及记事本文本区域均提供各自的右键菜单。
- “新建 → 文件夹 / 文本文档”直接在当前位置创建并就地编辑名称；Enter 确认、Esc 保留默认名称或取消重命名。文件名重名和非法字符会提示，重复新建自动编号。
- 可在文件夹、文件夹空白处、左侧导航目录、桌面和回收站之间拖拽移动。拖动过程中显示目标提示，Esc 取消；禁止移入自身或子目录。
- 右键、工具栏与 Ctrl+C / Ctrl+X / Ctrl+V 共用演示文件剪贴板。复制文件夹包含子目录和文本内容，重复粘贴自动编号，剪切粘贴保持文件身份。剪贴板仅驻留内存，刷新后清空。
- F2 重命名、Delete 移入回收站、Ctrl+Shift+N 新建文件夹。
- 新建的文本文档可双击或右键“用记事本打开”；编辑自动保存到同一个虚拟文件，标题显示文件名。记事本“文件”菜单可新建、保存及定位文件，“编辑”菜单和文本右键提供剪切、复制、粘贴、全选，“查看”菜单支持字体大小及自动换行。
- 文本右键使用 Demo 内的文本剪贴板；不是操作系统剪贴板。内部文件操作不会访问真实磁盘。

运行 `npm test` 检查名称冲突、递归复制、剪切身份及循环移动保护；运行 `npm run build` 做类型检查和生产构建。

## NAS 监控小组件

点击任务栏左下角的 NAS 状态入口，打开保留 Win11 左侧毛玻璃外框与卡片形式的小组件面板。点击外部或按 Esc 关闭。

- 存储池容量、RAID 5 状态、CPU / 内存占用、网络流量、服务与备份、最近活动。
- 四块硬盘的健康状态与温度；点击可展开序列号、通电时间、坏扇区和 S.M.A.R.T. 检测结果。
- 每 2.5 秒刷新模拟指标，可暂停 / 继续或手动刷新，支持 60 秒与 5 分钟趋势图。
- 搜索按硬盘 / 小组件名称筛选，未找到时显示空状态。
- 选择“模拟温度预警”，可查看 HDD 4 温度告警，任务栏入口与设备概览同步显示预警；切回“正常场景”恢复。
- 数据全部为浏览器生成的演示数据，不连接 NAS、不采集真实硬件指标。

交互外框参考：[Microsoft Windows Widgets](https://learn.microsoft.com/en-us/windows/apps/design/widgets/)。

## 图片查看

- 双击虚拟图片文件，或右键选择“用照片查看”，会打开独立的“照片”窗口；开始菜单的“照片”也可启动。
- 图片目录提供三张本地样例与缩略图，复制和移动后的图片仍指向同一图片资源。
- 支持适应窗口、实际大小、5%～500% 缩放、鼠标滚轮缩放、放大后拖动查看、顺时针旋转，以及同文件夹中的前后切换和缩略图导航。
- 快捷键：← / → 切换，+ / − 缩放，0 适应窗口，1 实际大小；双击图片切换实际大小与适应窗口。
- 工具栏可定位到图片所在文件夹，并把内置样例设为桌面背景。
- 旋转和缩放只影响查看状态，不修改图片文件。

## 云盘应用

双击桌面“云盘”打开，搜索框右侧“关联网盘”可添加 115、百度、夸克与本地 NAS。挂载账号由 Go 后端验证并保存在 SQLite，Token / Cookie 加密保存；旧版浏览器演示账号不导入后端。支持文件夹导航、当前目录筛选、收藏、排序、列表 / 网格切换、目录缓存与强制刷新、解除挂载。

本地目录支持真实图片 / 文本预览和文件下载；夸克支持代理下载。夸克和本地 NAS 支持右键新建文件夹、重命名、批量复制 / 剪切 / 粘贴及移入回收站。可在空白处拖动框选，Ctrl 点击增减选择、Shift 连选，支持 Ctrl+A/C/X/V、F2、Delete、Ctrl+Shift+N。剪切项变淡；同名项目拒绝覆盖，暂不支持跨网盘粘贴。115 / 百度自动授权需要配置兼容的可信 OAuth 服务，夸克提供扫码入口；未完成真实账号联调的远程操作仅经过模拟协议测试。运行和配置参见 [后端说明](api/README.md)。

选中文件/文件夹后，右键或工具栏“下载到本地”，选择已挂载的 NAS 和目标目录，支持同名跳过/自动重命名。左侧“传输任务”查看进度、速度、错误、暂停/继续、取消/重试、打开目标和移除记录。任务在 Go 后端运行，关闭窗口继续执行，最多并行 2 个任务；重启后可继续，已完成文件保留，当前文件重新下载。当前来源为夸克/本地 NAS，目标只支持运行后端的设备可访问的本地硬盘/映射 NAS。实现对照见 [传输设计](docs/local-transfer-design.md)。

## Linux 直接安装（无需 Nginx / Node / Go / Docker）

从 GitHub Releases 下载对应架构的 `webos-linux-amd64.tar.gz`（普通 x86 NAS / PC）或 `webos-linux-arm64.tar.gz`（ARM64）。前端已编译，Go 程序同时提供页面和 API。

以下示例在 `/opt/webos` 内完成下载、解压和安装；也可将第一行改成 `/home/你的用户名/webos` 或 `/data/webos`，选择当前用户有写权限的目录：

```bash
INSTALL=/opt/webos
mkdir -p "$INSTALL/tmp/package"
cd "$INSTALL/tmp"
# ARM64 设备将 amd64 改为 arm64
curl -fL https://github.com/a3318375/win11webos/releases/latest/download/webos-linux-amd64.tar.gz -o webos.tar.gz
curl -fL https://github.com/a3318375/win11webos/releases/latest/download/SHA256SUMS -o SHA256SUMS
mv webos.tar.gz webos-linux-amd64.tar.gz
sha256sum --ignore-missing -c SHA256SUMS
tar -xzf webos-linux-amd64.tar.gz -C package
bash package/install.sh "$INSTALL"
rm -f webos-linux-amd64.tar.gz SHA256SUMS
rm -r package
```

可在安装命令末尾指定端口，例如 `bash package/install.sh "$INSTALL" 8888`；默认 9999。已安装时可在 `config.sh` 设置 `export CLOUD_API_ADDR="127.0.0.1:8888"` 后停止、启动应用。

安装完成自动后台启动，访问 http://localhost:9999 。SSH 安装到 NAS 时，可在自己电脑执行 `ssh -L 9999:127.0.0.1:9999 用户名@NAS地址`，再访问此地址。当前尚无管理员登录，因此保留仅本机监听。

```bash
bash /opt/webos/webos.sh status
bash /opt/webos/webos.sh stop
bash /opt/webos/webos.sh start
```

后台运行不依赖终端保持连接；当前不注册开机自启动服务。升级前先 stop，再安装新版到同一目录，数据库、密钥和配置会保留。删除前先停止进程；备份 `data` 时必须同时保留数据库与密钥。

安装目录内容：

| 目录 / 文件 | 用途 |
| --- | --- |
| `bin`、`www` | 后端程序、前端静态文件 |
| `data` | SQLite、加密密钥、传输记录 |
| `logs` | 当前及上次启动日志；单次运行日志持续追加 |
| `tmp`、`cache`、`run` | 应用临时文件、缓存、PID |
| `storage` | 默认允许挂载的本地文件目录 |
| `config.sh` | 可选配置，升级保留 |

默认无需填写配置。要挂载已有 NAS 硬盘，在安装目录的 `config.sh` 中写入：

```bash
export CLOUD_LOCAL_ROOTS="/data:/st"
```

重启应用，然后在本地 NAS 关联弹窗直接填 `/data` 或 `/st`。物理安装填写实际服务器路径，无需容器映射。用户主动下载的文件及传输临时文件会写入所选目标磁盘，其他应用数据保持在安装目录。

### GitHub Release 构建

推送到 `main` 且此次推送最后一个提交信息包含 `[build]` 时，工作流测试前后端，交叉编译 Linux amd64 / arm64，验证 amd64 包安装和 HTTP 服务，再发布 Release（两种压缩包及 SHA256SUMS）。普通提交不发布，无需额外 Token，仓库需允许 Actions 写入 Contents。

```bash
git commit -m "feat: update application [build]"
```

### Windows 本地开发

运行 `deploy/dev-api.ps1` 启动后端，另开终端 `npm run dev -- --port 5173 --strictPort`。仅此开发脚本启用 `/data` → `D:\tmp\res\data` 的别名，正式安装不启用。
本地 NAS 现通过硬盘列表选择，取代手填路径。共享硬盘状态缓存每 5 分钟更新，休眠或状态未知不可选。Linux 直连 ATA 状态查询需要 hdparm 和设备查询权限；USB/RAID/网络盘及 Windows 尚不支持安全识别，不自动降级为活动。只列出 CLOUD_LOCAL_ROOTS 配置的存储目录。此机制限制选择动作，不会拦截已有挂载的文件读写或传输；状态缓存存在最长约5分钟延迟。
