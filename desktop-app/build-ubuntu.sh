#!/bin/bash
# 小智桌面端 Ubuntu 自动化打包脚本

set -e

echo "🚀 开始打包小智桌面端应用..."

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 打印带颜色的消息
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查系统依赖
check_dependencies() {
    print_status "检查系统依赖..."
    
    # 检查必要的包
    missing_packages=()
    
    if ! dpkg -l | grep -q "libgtk-3-dev"; then
        missing_packages+=("libgtk-3-dev")
    fi
    
    if ! dpkg -l | grep -q "libwebkit2gtk-4.0-dev"; then
        missing_packages+=("libwebkit2gtk-4.0-dev")
    fi
    
    if ! dpkg -l | grep -q "build-essential"; then
        missing_packages+=("build-essential")
    fi
    
    if [ ${#missing_packages[@]} -ne 0 ]; then
        print_warning "缺少以下依赖包: ${missing_packages[*]}"
        print_status "正在安装依赖包..."
        sudo apt update
        sudo apt install -y "${missing_packages[@]}"
    fi
    
    print_success "依赖检查完成"
}

# 检查 Wails 环境
check_wails() {
    print_status "检查 Wails 环境..."
    
    export PATH=$PATH:/home/vipas/go/bin
    
    if ! command -v wails &> /dev/null; then
        print_error "Wails 命令未找到，请确保已安装 Wails"
        exit 1
    fi
    
    # 运行 wails doctor
    print_status "运行 Wails 环境检查..."
    wails doctor || {
        print_warning "Wails 环境检查发现问题，但继续构建..."
    }
    
    print_success "Wails 环境检查完成"
}

# 构建应用
build_app() {
    print_status "开始构建应用..."
    
    cd /home/vipas/workspace/xiaozhi-server/xiaozhi-server/desktop-app/xiaozhi-desktop
    
    # 生成绑定
    print_status "生成 Wails 绑定..."
    export PATH=$PATH:/home/vipas/go/bin
    wails generate module
    
    # 构建前端
    print_status "构建前端..."
    cd frontend
    if [ ! -d "node_modules" ]; then
        print_status "安装前端依赖..."
        npm install
    fi
    npm run build
    cd ..
    
    # 构建应用
    print_status "构建桌面应用..."
    # 生成编译时间（格式：YYYY-MM-DD HH:MM:SS）
    BUILD_TIME=$(date '+%Y-%m-%d %H:%M:%S')
    print_status "编译时间: $BUILD_TIME"
    wails build -compress -clean -ldflags "-X 'main.BuildTime=$BUILD_TIME'"
    
    print_success "应用构建完成"
}

# 创建分发包
create_packages() {
    print_status "创建分发包..."
    
    # 创建输出目录
    mkdir -p ../dist
    cd ../dist
    
    # 复制可执行文件
    cp ../xiaozhi-desktop/build/bin/xiaozhi-desktop ./
    chmod +x xiaozhi-desktop
    
    # 创建 .deb 包
    create_deb_package
    
    # 创建 AppImage
    create_appimage
    
    print_success "分发包创建完成"
}

# 创建 .deb 包
create_deb_package() {
    print_status "创建 .deb 包..."
    
    # 创建 deb 包目录结构
    mkdir -p xiaozhi-desktop-deb/DEBIAN
    mkdir -p xiaozhi-desktop-deb/usr/bin
    mkdir -p xiaozhi-desktop-deb/usr/share/applications
    mkdir -p xiaozhi-desktop-deb/usr/share/pixmaps
    
    # 复制可执行文件
    cp xiaozhi-desktop xiaozhi-desktop-deb/usr/bin/
    
    # 创建桌面文件
    cat > xiaozhi-desktop-deb/usr/share/applications/xiaozhi-desktop.desktop << EOF
[Desktop Entry]
Name=小智桌面端
Comment=小智AI助手桌面应用
Exec=/usr/bin/xiaozhi-desktop
Icon=xiaozhi-desktop
Terminal=false
Type=Application
Categories=Utility;Development;
StartupWMClass=xiaozhi-desktop
EOF
    
    # 创建控制文件
    cat > xiaozhi-desktop-deb/DEBIAN/control << EOF
Package: xiaozhi-desktop
Version: 1.0.0
Section: utils
Priority: optional
Architecture: amd64
Maintainer: xiaozhi-team <xiaozhi@example.com>
Installed-Size: $(du -s xiaozhi-desktop-deb/usr | cut -f1)
Description: 小智AI助手桌面应用
 基于 Wails 框架开发的小智AI助手桌面端应用
 提供可视化的服务管理界面，支持管理 Backend、Manager、Redis 等服务
EOF
    
    # 设置权限
    chmod +x xiaozhi-desktop-deb/usr/bin/xiaozhi-desktop
    
    # 构建 deb 包
    dpkg-deb --build xiaozhi-desktop-deb xiaozhi-desktop_1.0.0_amd64.deb
    
    print_success ".deb 包创建完成: xiaozhi-desktop_1.0.0_amd64.deb"
}

# 创建 AppImage
create_appimage() {
    print_status "创建 AppImage..."
    
    # 检查 appimagetool
    if ! command -v appimagetool &> /dev/null; then
        print_warning "appimagetool 未安装，跳过 AppImage 创建"
        print_status "要创建 AppImage，请运行: sudo apt install appimagetool"
        return
    fi
    
    # 创建 AppImage 目录结构
    mkdir -p xiaozhi-desktop.AppDir/usr/bin
    mkdir -p xiaozhi-desktop.AppDir/usr/share/applications
    
    # 复制可执行文件
    cp xiaozhi-desktop xiaozhi-desktop.AppDir/usr/bin/
    
    # 创建 AppRun 脚本
    cat > xiaozhi-desktop.AppDir/AppRun << 'EOF'
#!/bin/bash
cd "$(dirname "$0")"
exec ./usr/bin/xiaozhi-desktop "$@"
EOF
    chmod +x xiaozhi-desktop.AppDir/AppRun
    
    # 创建 desktop 文件
    cat > xiaozhi-desktop.AppDir/xiaozhi-desktop.desktop << EOF
[Desktop Entry]
Name=小智桌面端
Comment=小智AI助手桌面应用
Exec=xiaozhi-desktop
Icon=xiaozhi-desktop
Type=Application
Categories=Utility;
EOF
    
    # 创建 AppImage
    appimagetool xiaozhi-desktop.AppDir xiaozhi-desktop-1.0.0-x86_64.AppImage
    
    print_success "AppImage 创建完成: xiaozhi-desktop-1.0.0-x86_64.AppImage"
}

# 显示结果
show_results() {
    print_success "🎉 打包完成！"
    echo ""
    echo "📦 生成的文件:"
    echo "  - 可执行文件: dist/xiaozhi-desktop"
    echo "  - Deb 包: dist/xiaozhi-desktop_1.0.0_amd64.deb"
    if [ -f "xiaozhi-desktop-1.0.0-x86_64.AppImage" ]; then
        echo "  - AppImage: dist/xiaozhi-desktop-1.0.0-x86_64.AppImage"
    fi
    echo ""
    echo "🚀 安装和使用:"
    echo "  - 安装 .deb 包: sudo dpkg -i xiaozhi-desktop_1.0.0_amd64.deb"
    echo "  - 运行 AppImage: ./xiaozhi-desktop-1.0.0-x86_64.AppImage"
    echo "  - 直接运行: ./xiaozhi-desktop"
    echo ""
    echo "📋 文件大小:"
    ls -lh xiaozhi-desktop*
}

# 主函数
main() {
    echo "=========================================="
    echo "   小智桌面端 Ubuntu 自动化打包脚本"
    echo "=========================================="
    echo ""
    
    # 检查是否在正确的目录
    if [ ! -f "xiaozhi-desktop/wails.json" ]; then
        print_error "请在 desktop-app 目录下运行此脚本"
        exit 1
    fi
    
    # 执行打包流程
    check_dependencies
    check_wails
    build_app
    create_packages
    show_results
}

# 运行主函数
main "$@"
