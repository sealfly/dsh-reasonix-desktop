Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them with the values from ProjectInfo.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
##
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the ProjectInfo file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "MyProject" # Default "{{.Name}}"
## !define INFO_COMPANYNAME    "MyCompany" # Default "{{.Info.CompanyName}}"
## !define INFO_PRODUCTNAME    "MyProduct" # Default "{{.Info.ProductName}}"
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "{{.Info.ProductVersion}}"
## !define INFO_COPYRIGHT      "Copyright" # Default "{{.Info.Copyright}}"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
## !define REQUEST_EXECUTION_LEVEL "admin"            # Default "admin"  see also https://nsis.sourceforge.io/Docs/Chapter4.html
####
## Include the wails tools
####
!include "wails_tools.nsh"

# ===== Lazy installer switch =====
# Two flavors:
#   classic : makensis project.nsi                    (no bundled DSH; online install)
#   lazy    : makensis -DBUNDLE_DSH project.nsi        (embeds DSH runtime + node.exe)
# Lazy runtime dir is prepared by prepare-dsh-runtime.ps1 -> dsh-runtime/:
#   dsh-runtime\node.exe           (node single exe to run dsh)
#   dsh-runtime\dsh\node_modules\  (@deepseek-ai/dsh full dependency tree)
!ifdef BUNDLE_DSH
  !ifndef BUNDLE_DSH_DIR
    !define BUNDLE_DSH_DIR "dsh-runtime"
  !endif
!endif


# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!insertmacro MUI_PAGE_COMPONENTS # Choose components (DSH backend optional)
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uinstalling page

!insertmacro MUI_LANGUAGE "English" # Set the Language of the installer

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
!ifdef BUNDLE_DSH
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer-lazy.exe" # lazy flavor (bundled DSH+node)
!else
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # classic flavor
!endif
!ifdef WAILS_INSTALL_SCOPE
  !if "${WAILS_INSTALL_SCOPE}" == "user"
    InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
  !else
    InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
  !endif
!else
  InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
!endif # Default installing folder ($PROGRAMFILES is Program Files folder).
ShowInstDetails show # This will always show the installation details.

Function .onInit
   !insertmacro wails.checkArchitecture
FunctionEnd

# ===== DSH 检测函数 =====
# $0 返回 "yes"/"no"

# 检测 DSH 是否已安装/运行
Function dsh.detectInstalled
    Push $R0
    # 1. 3080 端口活跃? (DSH 在跑)
    nsExec::ExecToStack "powershell -NoProfile -Command (Get-NetTCPConnection -LocalPort 3080 -State Listen -ErrorAction SilentlyContinue) -ne $$null"
    Pop $R0
    Pop $R0
    StrCmp $R0 "True" dshDetected yes

    dshDetected:
        StrCpy $0 "yes"
        Goto dshDetectDone

    yes:
        # 2. dsh 命令存在?
        nsExec::ExecToStack "where dsh"
        Pop $R0
        Pop $R0
        StrCmp $R0 "" noDsh yesDsh

        yesDsh:
            StrCpy $0 "yes"
            Goto dshDetectDone

        noDsh:
            StrCpy $0 "no"

    dshDetectDone:
    Pop $R0
FunctionEnd

# 检测 Node.js
Function node.detect
    Push $R0
    nsExec::ExecToStack "where node"
    Pop $R0
    Pop $R0
    StrCmp $R0 "" noNode yesNode

    yesNode:
        StrCpy $0 "yes"
        Goto nodeDetectDone

    noNode:
        StrCpy $0 "no"

    nodeDetectDone:
    Pop $R0
FunctionEnd

# ===== 组件: DSH-ReasonixUI 桌面客户端(必需) =====
Section "DSH-ReasonixUI 桌面客户端" SecApp
    SectionIn RO
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files

    # 复制 DSH 启动器(装 DSH 后端时用)
    File "/oname=start-dsh.cmd" "start-dsh.cmd"

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller
SectionEnd

# ===== Component: DSH backend (optional) =====
Section "DSH 后端 (DeepSeek Harness, 127.0.0.1:3080)" SecDSH
    SectionIn 1
    !insertmacro wails.setShellContext

    !ifdef BUNDLE_DSH
        # Lazy flavor: unpack bundled DSH runtime (node.exe + dsh tree) first
        SetOutPath "$INSTDIR"
        DetailPrint "Unpacking bundled DSH runtime (node.exe + dsh)..."
        File /r "${BUNDLE_DSH_DIR}"
    !endif

    # 1. Already installed or running?
    Call dsh.detectInstalled
    StrCmp $0 "yes" dshAlreadyInstalled dshDoInstall

    dshAlreadyInstalled:
        DetailPrint "DSH backend already present, skip install"
        Goto dshDone

    dshDoInstall:
        # 2. Node.js present? (online install needs it; lazy bundle does not)
        Call node.detect
        StrCmp $0 "yes" nodeFound nodeMissing

        nodeFound:
            DetailPrint "Node.js found, trying online install (npm latest stable)..."
            nsExec::ExecToLog "npm install -g @deepseek-ai/dsh --no-audit --no-fund --fetch-timeout=15000 --fetch-retries=1"
            Pop $0
            DetailPrint "npm exit code: $0"
            StrCmp $0 "0" dshInstalled npmFailed

        npmFailed:
            DetailPrint "Online install failed (offline?), falling back to bundled runtime"
            !ifdef BUNDLE_DSH
                IfFileExists "$INSTDIR\dsh-runtime\node.exe" dshInstalled noBundleFile
                noBundleFile:
                    DetailPrint "Error: bundled runtime missing (dsh-runtime\node.exe)"
                    Goto dshDone
            !else
                DetailPrint "No bundled DSH in this installer. Retry online or run: npm install -g @deepseek-ai/dsh"
                Goto dshDone
            !endif

        nodeMissing:
            !ifdef BUNDLE_DSH
                DetailPrint "Node.js not found, using bundled DSH runtime (includes node.exe)"
                IfFileExists "$INSTDIR\dsh-runtime\node.exe" dshInstalled noBundleNoNode
                noBundleNoNode:
                    DetailPrint "Error: bundled runtime missing (dsh-runtime\node.exe)"
                    Goto dshDone
            !else
                MessageBox MB_YESNO|MB_ICONINFORMATION "需要 Node.js 才能安装 DSH 后端。$\r$\n$\r$\n是否打开 Node.js 下载页? (https://nodejs.org)$\r$\n$\r$\n安装 Node.js 后重新运行本安装器勾选 DSH 即可。$\r$\n$\r$\n(离线环境请使用内置 DSH 的懒人包)" IDYES openNode IDNO skipNode
                openNode:
                    ExecShell "open" "https://nodejs.org"
                skipNode:
                Goto dshDone
            !endif

        dshInstalled:
            # 3. Shortcuts + auto start
            CreateShortCut "$DESKTOP\启动 DSH 后端.lnk" "$INSTDIR\start-dsh.cmd"
            CreateShortCut "$SMPROGRAMS\启动 DSH 后端.lnk" "$INSTDIR\start-dsh.cmd"
            ExecShell "open" "$INSTDIR\start-dsh.cmd"
            DetailPrint "DSH backend started (127.0.0.1:3080)"
        dshDone:
SectionEnd
Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"
    # DSH 后端启动快捷方式(装 DSH 时创建)
    Delete "$DESKTOP\启动 DSH 后端.lnk"
    Delete "$SMPROGRAMS\启动 DSH 后端.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller
SectionEnd
