[CmdletBinding()]
param(
    [ValidateSet("amd64", "arm64")]
    [string]$Arch = "amd64",

    [string]$BuildTags = "",

    [switch]$SkipFrontend,
    [switch]$SkipBindings,
    [switch]$SkipDoctor,
    [switch]$NoPackage
)

$ErrorActionPreference = "Stop"

function Write-Info([string]$Message) {
    Write-Host "[INFO ] $Message" -ForegroundColor Cyan
}

function Write-Success([string]$Message) {
    Write-Host "[ OK  ] $Message" -ForegroundColor Green
}

function Write-Warn([string]$Message) {
    Write-Host "[WARN ] $Message" -ForegroundColor Yellow
}

function Write-ErrorLine([string]$Message) {
    Write-Host "[ERROR] $Message" -ForegroundColor Red
}

function Require-Command([string]$Name, [string]$InstallHint) {
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        if ([string]::IsNullOrWhiteSpace($InstallHint)) {
            $InstallHint = $Name
        }
        throw "Command '$Name' not found. Install/configure: $InstallHint"
    }
}

function Invoke-Step([string]$Message, [scriptblock]$Action) {
    Write-Info $Message
    & $Action
    Write-Success "$Message completed"
}

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Definition
$projectDir = Join-Path $scriptRoot "xiaozhi-desktop"
$frontendDir = Join-Path $projectDir "frontend"
$distRoot = Join-Path $scriptRoot "dist"

if (-not (Test-Path $projectDir)) {
    throw "Desktop project directory not found: $projectDir"
}

Write-Info "Target arch: $Arch"
Write-Info "Project dir: $projectDir"
if ($BuildTags) {
    Write-Info "Build tags: $BuildTags"
}

Invoke-Step "Check prerequisites" {
    Require-Command -Name go -InstallHint "https://go.dev/doc/install"
    Require-Command -Name npm -InstallHint "https://nodejs.org/en/download"
    Require-Command -Name wails -InstallHint "go install github.com/wailsapp/wails/v2/cmd/wails@latest"

    $nodeVersion = (& npm --version)
    $goVersion = (& go version)
    Write-Info "Go: $goVersion"
    Write-Info "Node.js/NPM: $nodeVersion"
}

if (-not $SkipDoctor) {
    Invoke-Step "Run Wails doctor" {
        Push-Location $projectDir
        try {
            & wails doctor | Write-Verbose
        } catch {
            Write-Warn "Wails doctor warning: $($_.Exception.Message)"
        } finally {
            Pop-Location
        }
    }
} else {
    Write-Warn '跳过 Wails 环境检查 (SkipDoctor)'
}

if (-not $SkipBindings) {
    Invoke-Step "Generate Wails bindings" {
        Push-Location $projectDir
        try {
            & wails generate module
        } finally {
            Pop-Location
        }
    }
} else {
    Write-Warn '跳过绑定代码生成 (SkipBindings)'
}

if (-not $SkipFrontend) {
    Invoke-Step "Build frontend" {
        Push-Location $frontendDir
        try {
            $nodeModules = Join-Path $frontendDir "node_modules"
            if (-not (Test-Path $nodeModules)) {
                Write-Info "Installing frontend dependencies..."
                & npm install
            } else {
                Write-Info "Existing deps detected, skipping npm install. Remove node_modules to reinstall."
            }

            Write-Info "Running npm run build..."
            & npm run build
        } finally {
            Pop-Location
        }
    }
} else {
    Write-Warn 'Skip frontend build (SkipFrontend). Ensure frontend/dist exists.'
}

Invoke-Step "Build Windows app" {
    Push-Location $projectDir
    try {
        # 生成编译时间（格式：YYYY-MM-DD HH:MM:SS）
        # 使用 cmd 命令获取日期时间，避免 PowerShell 变量作用域问题
        $datePart = (Get-Date -Format "yyyy-MM-dd")
        $timePart = (Get-Date -Format "HH:mm:ss")
        $buildTimeStr = "$datePart $timePart"
        
        Write-Info "Build time: $buildTimeStr"
        
        # 构建 ldflags 参数
        $ldflagsValue = "-X `"main.BuildTime=$buildTimeStr`""
        Write-Info "LDFlags: $ldflagsValue"
        
        # 构建 wails build 命令
        if ($BuildTags) {
            Write-Info "Using build tags: $BuildTags"
            & wails build -clean -platform "windows/$Arch" -tags $BuildTags -ldflags $ldflagsValue
        } else {
            & wails build -clean -platform "windows/$Arch" -ldflags $ldflagsValue
        }
    } finally {
        Pop-Location
    }
}

# 如果使用了 sherpa_onnx build tag，需要复制 DLL 文件
if ($BuildTags -and $BuildTags.Contains("sherpa_onnx")) {
    Invoke-Step "Copy DLLs for sherpa_onnx" {
        Push-Location $projectDir
        try {
            $goPath = & go env GOPATH
            $moduleRoot = Join-Path $goPath "pkg/mod/github.com/k2-fsa"
            
            if (Test-Path $moduleRoot) {
                $dllModule = Get-ChildItem -Path $moduleRoot -Directory -Filter "sherpa-onnx-go-windows@*" -ErrorAction SilentlyContinue | 
                    Sort-Object Name -Descending | Select-Object -First 1
                
                if ($dllModule) {
                    $dllDir = Join-Path $dllModule.FullName "lib/x86_64-pc-windows-gnu"
                    
                    if (Test-Path $dllDir) {
                        # 查找构建输出目录
                        $buildBinDirs = @(
                            (Join-Path $projectDir "build\bin"),
                            (Join-Path $projectDir "build\bin\windows\$Arch"),
                            (Join-Path $projectDir "build\bin\windows-$Arch")
                        )
                        
                        foreach ($binDir in $buildBinDirs) {
                            if (Test-Path $binDir) {
                                Write-Info "Copying DLLs to $binDir"
                                $dllFiles = Get-ChildItem -Path $dllDir -Filter "*.dll" -ErrorAction SilentlyContinue
                                if ($dllFiles) {
                                    Copy-Item -Path (Join-Path $dllDir "*.dll") -Destination $binDir -Force
                                    Write-Success "Copied $($dllFiles.Count) DLL file(s)"
                                } else {
                                    Write-Warn "No DLL files found in $dllDir"
                                }
                            }
                        }
                    } else {
                        Write-Warn "DLL directory not found: $dllDir"
                    }
                } else {
                    Write-Warn "sherpa-onnx-go-windows module not found in $moduleRoot"
                }
            } else {
                Write-Warn "Go module root not found: $moduleRoot"
            }
        } finally {
            Pop-Location
        }
    }
}

$binRoot = Join-Path $projectDir "build\bin"
$expectedPaths = @(
    (Join-Path $binRoot "xiaozhi-desktop.exe"),
    (Join-Path $binRoot "windows\$Arch\xiaozhi-desktop.exe"),
    (Join-Path $binRoot "windows-$Arch\xiaozhi-desktop.exe")
)

$binaryFile = $null
foreach ($candidate in $expectedPaths) {
    if (Test-Path $candidate) {
        $binaryFile = $candidate
        break
    }
}

if (-not $binaryFile) {
    $candidates = Get-ChildItem -Path $binRoot -Filter "xiaozhi-desktop*.exe" -Recurse -ErrorAction Ignore | Sort-Object LastWriteTime -Descending
    if ($candidates) {
        $binaryFile = $candidates[0].FullName
    }
}

if (-not $binaryFile) {
    throw "Could not find xiaozhi-desktop executable in $binRoot. Check wails output."
}

$binaryDir = Split-Path $binaryFile -Parent
Write-Info "Build output dir: $binaryDir"

$outputDir = Join-Path $distRoot ("windows-$Arch")
if (Test-Path $outputDir) {
    Remove-Item $outputDir -Recurse -Force
}
New-Item -Path $outputDir -ItemType Directory | Out-Null

Write-Info "Copy build artifacts to $outputDir"
Copy-Item -Path (Join-Path $binaryDir "*") -Destination $outputDir -Recurse -Force

if (-not $NoPackage) {
    $zipName = "xiaozhi-desktop-windows-$Arch.zip"
    $zipPath = Join-Path $distRoot $zipName
    if (Test-Path $zipPath) {
        Remove-Item $zipPath -Force
    }
    Write-Info "Create archive $zipPath"
    Compress-Archive -Path (Join-Path $outputDir "*") -DestinationPath $zipPath
    Write-Success "Archive created: $zipPath"
} else {
    Write-Warn '已跳过压缩包生成 (NoPackage)'
}

Write-Success "Windows build completed. Artifacts at: $outputDir"
Write-Info "If WebView2 Runtime is missing, install from https://developer.microsoft.com/microsoft-edge/webview2/."
