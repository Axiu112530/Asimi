; Build with scripts\build-installer.ps1.
[Setup]
AppId={{89F2F31F-83B1-4F15-A404-B1BF9AF913A7}
AppName=Asimi
AppVersion={#AppVersion}
AppPublisher=Asimi
AppPublisherURL=https://github.com/Axiu112530/Asimi
DefaultDirName={localappdata}\Programs\Asimi
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
DisableWelcomePage=no
DisableDirPage=no
DisableProgramGroupPage=yes
DefaultGroupName=Asimi
LanguageDetectionMethod=none
ShowLanguageDialog=yes
WizardStyle=modern
WizardSizePercent=130,120
SetupIconFile={#SourcePath}asimi.ico
UninstallDisplayIcon={app}\Asimi.exe
VersionInfoDescription=Asimi Setup
VersionInfoVersion={#AppVersion}
CloseApplications=yes
RestartApplications=no
Compression=lzma2
SolidCompression=yes
SourceDir={#PayloadDir}
OutputBaseFilename=Asimi-{#AppVersion}-Setup-x64

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
#ifdef IncludeChinese
Name: "chinesesimplified"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"
#endif

[Messages]
english.WelcomeLabel2=Setup will install [name] in the folder you select.%n%nWhen upgrading, close Asimi before continuing.
#ifdef IncludeChinese
chinesesimplified.WelcomeLabel2=将在你选择的文件夹中安装 [name]。%n%n如需升级，请先关闭 Asimi。
chinesesimplified.SelectTasksLabel2=如需桌面快捷方式，请勾选下方选项。%n点击“下一步”继续。
chinesesimplified.FinishedLabel=已安装 [name]。%n可从开始菜单打开。
chinesesimplified.FinishedLabelNoIcons=已安装 [name]。
#endif

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "Asimi.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "AsimiControlHost.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "AsimiControlHost.exe.config"; DestDir: "{app}"; Flags: ignoreversion
Source: "AsimiUIAWorker.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "AsimiUIAWorker.exe.config"; DestDir: "{app}"; Flags: ignoreversion
Source: "INSTALLATION-GUIDE.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "USER-GUIDE.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "UPGRADE-GUIDE.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "KNOWN-LIMITATIONS.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "THIRD-PARTY-NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "PRIVACY.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "SHA256SUMS.txt"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Asimi"; Filename: "{app}\Asimi.exe"; WorkingDir: "{app}"
Name: "{autodesktop}\Asimi"; Filename: "{app}\Asimi.exe"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
Filename: "{app}\Asimi.exe"; WorkingDir: "{app}"; Description: "{cm:LaunchProgram,Asimi}"; Flags: nowait postinstall skipifsilent unchecked

[CustomMessages]
english.ExistingInstallDir=Asimi is already installed in %1. Select that folder to upgrade. To install Asimi elsewhere, uninstall the existing copy first.
#ifdef IncludeChinese
chinesesimplified.ExistingInstallDir=Asimi 已安装在 %1。请选择该文件夹进行升级。如需更改安装位置，请先卸载现有版本。
#endif

[Code]
procedure InitializeWizard;
begin
  { Inno 6.7.4 reserves the cached system bitmap width, which can stay at
    96 DPI while the themed checkbox is larger. Give both checklists a
    scaled gutter so the native glyph stays inside the client rectangle. }
  WizardForm.TasksList.Offset := ScaleX(16);
  WizardForm.RunList.Offset := ScaleX(16);
end;

function CheckInstallDir: String;
var
  PreviousDir: String;
begin
  Result := '';
  PreviousDir := GetPreviousData('InstallDir', '');
  if (PreviousDir <> '') and
     not SameText(RemoveBackslashUnlessRoot(ExpandFileName(WizardDirValue)),
                  RemoveBackslashUnlessRoot(ExpandFileName(PreviousDir))) then
    Result := FmtMessage(CustomMessage('ExistingInstallDir'), [PreviousDir]);
end;

function NextButtonClick(CurPageID: Integer): Boolean;
var
  Error: String;
begin
  Result := True;
  if CurPageID = wpSelectDir then
  begin
    Error := CheckInstallDir;
    if Error <> '' then
    begin
      SuppressibleMsgBox(Error, mbError, MB_OK, IDOK);
      Result := False;
    end;
  end;
end;

procedure RegisterPreviousData(PreviousDataKey: Integer);
begin
  SetPreviousData(PreviousDataKey, 'InstallDir', WizardDirValue);
end;
