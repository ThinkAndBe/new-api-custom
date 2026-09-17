# erke-config-tool

ERKE AI 一键配置工具：**原生 Windows 界面**（Win32 控件，非浏览器），输入
使用教程页生成的 6 位配置码，选择 WorkBuddy / CodeBuddy，点「一键配置」即
写入对应的 models.json。

- 单文件 exe，原生窗口（walk/Win32），无控制台、无浏览器、无运行时依赖
- exe 本身**不含任何密钥**；配置内容由服务端 `/api/usage/guide_config` 按
  链接里的 token 实时生成（模型白名单过滤、参数齐全）
- 模型更新后重新运行一次工具即可刷新配置
- Windows 11 风格：DWM 圆角 + 暗色标题栏/客户区跟随系统（亮暗双模式）、
  Segoe UI Variable 字体（`win11style.go`，纯 syscall 无额外依赖）

## models.json 格式铁律（2026-09-17 修复）

**顶层必须是裸数组 `[{...}]`，绝不能写成 `{"models":[...]}`。**

原因：WorkBuddy 主进程每次启动都跑硬件白名单校验（`LocalModelHardwareGate`），校验不通过时调用
`purgeLocalModelsOnGateFail()` 清理本地模型；该函数**只认裸数组**，遇到对象包裹格式会判定为空并把
整个 `models.json` 原子重写为 `[]` —— 用户配置就丢了，而且日志还谎报 `Purge complete: 0 models removed`。
（WorkBuddy 的 UI / daemon 保存时写的都是裸数组，所以只有"手写配置"和"旧版工具"会踩到。）

本工具 v2.2 起的行为：
- 写入统一用**裸数组**（原子写 + 写完自检首字符为 `[`）
- 写入前把既有的对象包裹格式**归一化**过来
- 按 `id` 合并：只更新配置码里的模型，**不会删掉用户自己加的其它模型**
- 写入前自动备份为 `models.json.bak-YYYYmmdd-HHMMSS`

命令行（修复存量 / 体检，不需要配置码）：

```bash
erke-config-tool.exe --check                 # 体检：报告当前格式与风险
erke-config-tool.exe --normalize             # 把对象包裹格式就地修复为裸数组（自动备份）
erke-config-tool.exe --normalize --product codebuddy
```

## 构建

需要 Windows + Go 1.25+（实测 `CGO_ENABLED=0` 即可构建：walk 走 x/sys，不需要 gcc）。
资源 syso（comctl32 v6 清单，控制现代样式）已提交，无需重复生成；
若修改 app.manifest 则重新生成：
`go run github.com/akavel/rsrc -manifest app.manifest -o rsrc_windows_amd64.syso`

```bash
cd tools/erke-config-tool
CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -H windowsgui -X main.serverBase=https://tokenhub.erke.com" -o erke-config-tool.exe
```

测试可用环境变量覆盖服务器地址：ERKE_CONFIG_SERVER=http://127.0.0.1:3000

## 部署（管理员）

把 `erke-config-tool.exe` 放到 new-api 工作目录的 `config-tool/` 下
（docker 部署挂载到容器 `/app/config-tool/`，或数据卷 `/data/config-tool/`），
使用教程页的「下载配置工具」按钮即可下发。

## 构建（注入服务器地址）

```bash
cd tools/erke-config-tool
go build -trimpath -ldflags "-s -w -H windowsgui -X main.serverBase=https://tokenhub.erke.com" -o erke-config-tool.exe
```

## 用户流程（短码模式，最简）

1. 教程页点「下载配置工具」→ 双击运行（仅首次需要下载）
2. 教程页点「生成 WorkBuddy/CodeBuddy 配置码」→ 弹窗显示 6 位码（5 分钟有效，一次性）
3. 把 6 位码填进工具 → 点「一键配置」→ ✅ 完成（教程页码字号可点击复制）
4. 重启 WorkBuddy / CodeBuddy 生效

高级链接粘贴入口已移除，仅保留配置码方式。
