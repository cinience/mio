# README

## About

This is the official Wails Vue template.

You can configure the project by editing `wails.json`. More information about the project settings can be found
here: https://wails.io/docs/reference/project-config

## Live Development

To run in live development mode, run `wails dev` in the project directory. This will run a Vite development
server that will provide very fast hot reload of your frontend changes. If you want to develop in a browser
and have access to your Go methods, there is also a dev server that runs on http://localhost:34115. Connect
to this in your browser, and you can call your Go code from devtools.

## Building

To build a redistributable, production mode package, use `wails build`.

## Windows 构建

要在 Windows 上编译和打包桌面应用，可在 `desktop-app` 目录下使用 PowerShell 脚本：

```powershell
cd desktop-app
pwsh ./build-windows.ps1        # 默认生成 windows/amd64 架构包
pwsh ./build-windows.ps1 -Arch arm64   # 生成 ARM64 版本
```

脚本会自动执行以下步骤：

- 校验 `go`、`npm`、`wails` 等依赖
- （可选）运行 `wails doctor` 做环境检测
- 生成 Wails 绑定代码
- 安装/构建前端资源（如已存在可通过 `-SkipFrontend` 跳过）
- 调用 `wails build -platform windows/<arch>` 生成可执行文件
- 将产物复制到 `desktop-app/dist/windows-<arch>` 并打包为 `xiaozhi-desktop-windows-<arch>.zip`

常用可选参数：

- `-SkipFrontend`：跳过前端构建（需确保已有 `frontend/dist`）
- `-SkipBindings`：跳过 `wails generate module`
- `-SkipDoctor`：跳过环境检查
- `-NoPackage`：仅复制产物，不生成 zip 包

首次在 Windows 运行应用时，建议安装 Microsoft WebView2 Runtime：
https://developer.microsoft.com/microsoft-edge/webview2/
