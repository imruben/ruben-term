; ruben-term's Windows installer. Built by the release workflow:
;   makensis /DVERSION=1.2.3 scripts\installer.nsi  →  build\bin\ruben-term-1.2.3-setup.exe
; Paths are relative to this file (makensis works from its folder).
;
; It installs for the current user only (%LOCALAPPDATA%\Programs\ruben-term, no admin
; prompt), adds a Start menu shortcut and an entry in Settings → Apps to uninstall it.
; The app updates itself by running it as `setup.exe /S /relaunch`: no window, it waits
; for ruben-term to close, installs, and opens it again.

Unicode true
!include "MUI2.nsh"
!include "FileFunc.nsh"

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\ruben-term"

Name "ruben-term"
OutFile "..\build\bin\ruben-term-${VERSION}-setup.exe"
InstallDir "$LOCALAPPDATA\Programs\ruben-term"
RequestExecutionLevel user
SetCompressor /SOLID lzma
BrandingText "ruben-term ${VERSION}"

VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "ruben-term"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "FileDescription" "ruben-term installer"
VIAddVersionKey "LegalCopyright" "Ruben"

!define MUI_ICON "..\build\windows\icon.ico"
!define MUI_UNICON "..\build\windows\icon.ico"
!define MUI_ABORTWARNING
!define MUI_WELCOMEPAGE_TEXT "This installs ruben-term ${VERSION}, a terminal for developers.$\r$\n$\r$\nNo administrator rights are needed: it is installed for you only."
!define MUI_FINISHPAGE_RUN "$INSTDIR\ruben-term.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Open ruben-term"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

; ruben-term.exe cannot be replaced while it runs. When updating, the app quits right
; after starting this, so wait a little; after ~10 s, ask (silent: give up).
Function WaitForApp
  StrCpy $1 0
  loop:
    IfFileExists "$INSTDIR\ruben-term.exe" 0 done
    ClearErrors
    FileOpen $0 "$INSTDIR\ruben-term.exe" a
    IfErrors 0 free
    IntOp $1 $1 + 1
    IntCmp $1 20 ask 0 ask
    Sleep 500
    Goto loop
  ask:
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "ruben-term is open. Close it and click Retry." /SD IDCANCEL IDRETRY retry
    Abort
  retry:
    StrCpy $1 0
    Goto loop
  free:
    FileClose $0
  done:
FunctionEnd

Section "ruben-term"
  Call WaitForApp
  SetOutPath "$INSTDIR"
  File "..\build\bin\ruben-term.exe"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; the shortcut (and the relaunch below) start in your home folder
  SetOutPath "$PROFILE"
  CreateShortcut "$SMPROGRAMS\ruben-term.lnk" "$INSTDIR\ruben-term.exe"

  WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "ruben-term"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "Ruben"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\ruben-term.exe"
  WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"

  ${GetParameters} $R0
  ClearErrors
  ${GetOptions} $R0 "/relaunch" $R1
  IfErrors +2
    Exec '"$INSTDIR\ruben-term.exe"'
SectionEnd

; Your settings (%AppData%\ruben-term) and the shell files in WSL are kept.
Section "Uninstall"
  Delete "$INSTDIR\ruben-term.exe"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\ruben-term.lnk"
  DeleteRegKey HKCU "${UNINST_KEY}"
SectionEnd
