# erke-config-tool

ERKE AI 一键配置工具：图形界面（双击打开浏览器即界面），粘贴使用教程页复制的
配置链接，选择 WorkBuddy / CodeBuddy，点「一键配置」即写入对应的 models.json。

- 单文件 exe，无控制台窗口，无运行时依赖
- exe 本身**不含任何密钥**；配置内容由服务端 `/api/usage/guide_config` 按
  链接里的 token 实时生成（模型白名单过滤、参数齐全）
- 模型更新后重新运行一次工具即可刷新配置

## 构建

需要 Windows + Go 1.25+：

```bash
cd tools/erke-config-tool
go build -trimpath -ldflags "-s -w -H windowsgui" -o erke-config-tool.exe
```

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
3. 把 6 位码填进工具 → 点「一键配置」→ ✅ 完成
4. 重启 WorkBuddy / CodeBuddy 生效

高级折叠项里保留直接粘贴完整链接的方式（guide_config?...&key=...）。
