; Vitrine's Windows installer (Inno Setup 6). Built by packaging/windows/build.sh, which lays the files out in
; Stage first: Vitrine.exe (the notification-area program), bin\vitrine.exe (the command line), tools\ffmpeg and
; tools\vips (ffmpeg and libvips, LGPL builds) and licenses\.
;
; For this user only (no administrator needed): the program goes in %LOCALAPPDATA%\Programs\Vitrine, the settings in
; %APPDATA%\vitrine\config.toml (where the vitrine command looks too), the library where the user picks. Updating -
; the in-app Update runs this installer with /VERYSILENT - keeps the settings and the library and starts Vitrine again.
;
; Unattended installs can set the first install's choices: /DATADIR=, /FILESDIR=, /BACKUPDIR= (the folders), /PORT=
; and /PHONES=0 (this computer only), with Inno Setup's own /SILENT, /DIR= and /TASKS= (startup, path, desktopicon).

#ifndef Version
  #define Version "0.0.0"
#endif
#ifndef NumVersion
  #define NumVersion "0.0.0.0"
#endif
#ifndef Stage
  #error Stage (the folder with the files to install) is not set
#endif

#define AppId "{{6F1C3A52-8D4B-4E27-9B0E-5A7C2D1E9F34}"

[Setup]
AppId={#AppId}
AppName=Vitrine
AppVersion={#Version}
AppVerName=Vitrine {#Version}
AppPublisher=Vitrine
AppPublisherURL=https://vitrine.software
AppSupportURL=https://github.com/usevitrine/vitrine/issues
AppUpdatesURL=https://github.com/usevitrine/vitrine/releases
AppCopyright=Vitrine contributors, AGPL-3.0-or-later
VersionInfoVersion={#NumVersion}
PrivilegesRequired=lowest
DefaultDirName={autopf}\Vitrine
DisableProgramGroupPage=yes
DisableReadyPage=no
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0.17763
OutputBaseFilename=Vitrine-Setup-{#Version}-x64
SetupIconFile=..\..\cmd\vitrine\vitrine.ico
UninstallDisplayIcon={app}\Vitrine.exe
UninstallDisplayName=Vitrine
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
WizardSizePercent=110
WizardSmallImageFile=wizard-small.png,wizard-small@2x.png
WizardImageFile=wizard-large.png,wizard-large@2x.png
ChangesEnvironment=yes
; (a running Vitrine is asked to stop - Vitrine.exe --quit - so it closes its library cleanly)
CloseApplications=no

[Messages]
FinishedLabel=Vitrine is installed. It runs in the notification area (by the clock) and opens in your browser - click its icon there to open it again.

[CustomMessages]
DataTitle=Your library
DataDescription=Where should Vitrine keep your library?
DataSub=You can move these later in config.toml (Vitrine's settings file).
DataLibrary=&Library: databases, thumbnails and previews (best on a fast drive)
DataFiles=&Original files: everything you add or download (can be a big, slower drive)
DataBackups=&Backups of the library database (another drive is safer)
DataQuote=A folder's name can't contain an apostrophe (').
NetTitle=Network
NetDescription=How do you want to open Vitrine?
NetPort=&Port (the address is http://localhost:PORT):
NetPhones=Let &phones and other devices on my Wi-Fi open Vitrine
NetPhonesHint=Allowed on private networks only (Windows asks for permission once). Without it, only this computer can open Vitrine.
NetLocal=Only this computer can open Vitrine
NetBadPort=Pick a port from 1024 to 65535.
TaskStartup=&Start Vitrine when I sign in to Windows
TaskPath=Add the &vitrine command to PATH (for the command line)
TaskGroup=Options:
OpenNow=Open Vitrine
RemoveLibrary=Also delete your library?%n%nThis deletes Vitrine's databases, thumbnails and previews (and what else is in that folder):%n%1%n%nIt can't be undone. Choose No to keep it (installing Vitrine again picks it up).
RemoveFiles=Delete this folder too?%n%n%1%n%n%2 It can't be undone.
RemoveFilesWhat=Your original files: everything you added or downloaded.
RemoveBackupsWhat=The backups of your library's database.

[Tasks]
Name: startup; Description: "{cm:TaskStartup}"; GroupDescription: "{cm:TaskGroup}"; Check: not IsUpgrade
Name: path; Description: "{cm:TaskPath}"; GroupDescription: "{cm:TaskGroup}"
Name: desktopicon; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:TaskGroup}"; Flags: unchecked

[Files]
Source: "{#Stage}\Vitrine.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#Stage}\bin\*"; DestDir: "{app}\bin"; Flags: ignoreversion
Source: "{#Stage}\tools\*"; DestDir: "{app}\tools"; Flags: ignoreversion recursesubdirs
Source: "{#Stage}\licenses\*"; DestDir: "{app}\licenses"; Flags: ignoreversion
Source: "{#Stage}\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#Stage}\config.example.toml"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Vitrine"; Filename: "{app}\Vitrine.exe"; Comment: "Open Vitrine"
Name: "{autodesktop}\Vitrine"; Filename: "{app}\Vitrine.exe"; Tasks: desktopicon

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "Vitrine"; ValueData: """{app}\Vitrine.exe"" --background"; Tasks: startup

[Run]
Filename: "{app}\Vitrine.exe"; Description: "{cm:OpenNow}"; Flags: nowait postinstall skipifsilent
; (the in-app Update: Vitrine back, without opening another browser tab - the page reloads itself)
Filename: "{app}\Vitrine.exe"; Parameters: "--background"; Flags: nowait; Check: WizardSilent

[UninstallRun]
Filename: "{app}\Vitrine.exe"; Parameters: "--quit"; Flags: runhidden waituntilterminated; RunOnceId: "QuitVitrine"

[Code]
const
  StateKey = 'Software\Vitrine';
  RunKey = 'Software\Microsoft\Windows\CurrentVersion\Run';
  UninstallKey = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{#AppId}_is1';

var
  DataPage: TInputDirWizardPage;
  NetPage: TWizardPage;
  PortEdit: TNewEdit;
  PhonesCheck: TNewCheckBox;
  LastData: String;

function ConfigFile: String;
begin
  Result := ExpandConstant('{userappdata}\vitrine\config.toml');
end;

{ An update (Vitrine installed already): the settings and the library stay as they are. }
function IsUpgrade: Boolean;
begin
  Result := RegKeyExists(HKCU, UninstallKey) or FileExists(ConfigFile);
end;

{ The files and backups folders follow the library folder while they're still inside it. }
procedure DataChanged(Sender: TObject);
begin
  if (DataPage.Values[1] = '') or (CompareText(DataPage.Values[1], AddBackslash(LastData) + 'files') = 0) then
    DataPage.Values[1] := AddBackslash(DataPage.Values[0]) + 'files';
  if (DataPage.Values[2] = '') or (CompareText(DataPage.Values[2], AddBackslash(LastData) + 'backups') = 0) then
    DataPage.Values[2] := AddBackslash(DataPage.Values[0]) + 'backups';
  LastData := DataPage.Values[0];
end;

procedure InitializeWizard;
var
  PortLabel, Hint: TNewStaticText;
begin
  DataPage := CreateInputDirPage(wpSelectDir, CustomMessage('DataTitle'), CustomMessage('DataDescription'),
    CustomMessage('DataSub'), False, 'Vitrine');
  DataPage.Add(CustomMessage('DataLibrary'));
  DataPage.Add(CustomMessage('DataFiles'));
  DataPage.Add(CustomMessage('DataBackups'));
  LastData := ExpandConstant('{param:DATADIR|{localappdata}\Vitrine}');
  DataPage.Values[0] := LastData;
  DataPage.Values[1] := ExpandConstant('{param:FILESDIR|' + LastData + '\files}');
  DataPage.Values[2] := ExpandConstant('{param:BACKUPDIR|' + LastData + '\backups}');
  DataPage.Edits[0].OnChange := @DataChanged;

  NetPage := CreateCustomPage(DataPage.ID, CustomMessage('NetTitle'), CustomMessage('NetDescription'));
  PortLabel := TNewStaticText.Create(NetPage);
  PortLabel.Parent := NetPage.Surface;
  PortLabel.Caption := CustomMessage('NetPort');
  PortEdit := TNewEdit.Create(NetPage);
  PortEdit.Parent := NetPage.Surface;
  PortEdit.Top := PortLabel.Top + PortLabel.Height + ScaleY(6);
  PortEdit.Width := ScaleX(90);
  PortEdit.Text := ExpandConstant('{param:PORT|8080}');
  PortLabel.FocusControl := PortEdit;
  PhonesCheck := TNewCheckBox.Create(NetPage);
  PhonesCheck.Parent := NetPage.Surface;
  PhonesCheck.Top := PortEdit.Top + PortEdit.Height + ScaleY(22);
  PhonesCheck.Width := NetPage.SurfaceWidth;
  PhonesCheck.Height := ScaleY(20);
  PhonesCheck.Caption := CustomMessage('NetPhones');
  PhonesCheck.Checked := ExpandConstant('{param:PHONES|1}') <> '0';
  Hint := TNewStaticText.Create(NetPage);
  Hint.Parent := NetPage.Surface;
  Hint.Top := PhonesCheck.Top + PhonesCheck.Height + ScaleY(4);
  Hint.Left := ScaleX(18);
  Hint.Width := NetPage.SurfaceWidth - ScaleX(18);
  Hint.AutoSize := False;
  Hint.WordWrap := True;
  Hint.Height := ScaleY(40);
  Hint.Caption := CustomMessage('NetPhonesHint');
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := ((PageID = DataPage.ID) or (PageID = NetPage.ID)) and IsUpgrade;
end;

function NextButtonClick(CurPageID: Integer): Boolean;
var
  I, Port: Integer;
begin
  Result := True;
  if CurPageID = DataPage.ID then
    for I := 0 to 2 do
      if Pos('''', DataPage.Values[I]) > 0 then begin
        MsgBox(CustomMessage('DataQuote'), mbError, MB_OK);
        Result := False;
        Exit;
      end;
  if CurPageID = NetPage.ID then begin
    Port := StrToIntDef(Trim(PortEdit.Text), 0);
    if (Port < 1024) or (Port > 65535) then begin
      MsgBox(CustomMessage('NetBadPort'), mbError, MB_OK);
      Result := False;
    end;
  end;
end;

function UpdateReadyMemo(Space, NewLine, MemoUserInfoInfo, MemoDirInfo, MemoTypeInfo, MemoComponentsInfo,
  MemoGroupInfo, MemoTasksInfo: String): String;
begin
  Result := MemoDirInfo;
  if not IsUpgrade then begin
    Result := Result + NewLine + NewLine + CustomMessage('DataTitle') + ':' + NewLine + Space + DataPage.Values[0]
      + NewLine + Space + DataPage.Values[1] + NewLine + Space + DataPage.Values[2];
    Result := Result + NewLine + NewLine + CustomMessage('NetTitle') + ':' + NewLine + Space + 'http://localhost:' + Trim(PortEdit.Text);
    if PhonesCheck.Checked then
      Result := Result + NewLine + Space + CustomMessage('NetPhones')
    else
      Result := Result + NewLine + Space + CustomMessage('NetLocal');
  end;
  if MemoTasksInfo <> '' then
    Result := Result + NewLine + NewLine + MemoTasksInfo;
  StringChangeEx(Result, '&', '', True);
end;

{ A running Vitrine stops first (it closes its library cleanly); an update started from it has already. }
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Code: Integer;
begin
  Result := '';
  if FileExists(ExpandConstant('{app}\Vitrine.exe')) then
    Exec(ExpandConstant('{app}\Vitrine.exe'), '--quit', '', SW_HIDE, ewWaitUntilTerminated, Code);
end;

procedure WriteConfig;
var
  Lines: TArrayOfString;
  Listen: String;
begin
  Listen := ':' + Trim(PortEdit.Text);
  if not PhonesCheck.Checked then
    Listen := '127.0.0.1' + Listen;
  SetArrayLength(Lines, 9);
  Lines[0] := '# Vitrine''s settings, written by the installer. All of them: config.example.toml in';
  Lines[1] := '# ' + ExpandConstant('{app}') + '. Quit and start Vitrine again after a change.';
  Lines[2] := '';
  Lines[3] := 'data_dir = ''' + DataPage.Values[0] + '''';
  Lines[4] := 'files_dir = ''' + DataPage.Values[1] + '''';
  Lines[5] := 'backup_dir = ''' + DataPage.Values[2] + '''';
  Lines[6] := '';
  Lines[7] := '# The web app''s address (":8080": every network - phones too; "127.0.0.1:8080": this computer only)';
  Lines[8] := 'listen = ''' + Listen + '''';
  ForceDirectories(ExtractFileDir(ConfigFile));
  SaveStringsToUTF8FileWithoutBOM(ConfigFile, Lines, False);
  RegWriteStringValue(HKCU, StateKey, 'DataDir', DataPage.Values[0]);
  RegWriteStringValue(HKCU, StateKey, 'FilesDir', DataPage.Values[1]);
  RegWriteStringValue(HKCU, StateKey, 'BackupDir', DataPage.Values[2]);
end;

{ Lets Vitrine through Windows' firewall on private networks (asks for administrator permission once). }
procedure AllowPhones;
var
  Code: Integer;
  Rule: String;
begin
  Rule := 'advfirewall firewall add rule name="Vitrine" dir=in action=allow profile=private enable=yes program="'
    + ExpandConstant('{app}\Vitrine.exe') + '"';
  if ShellExec('runas', 'netsh.exe', Rule, '', SW_HIDE, ewWaitUntilTerminated, Code) and (Code = 0) then
    RegWriteDWordValue(HKCU, StateKey, 'FirewallRule', 1);
end;

function PathEntries: String;
begin
  if not RegQueryStringValue(HKCU, 'Environment', 'Path', Result) then
    Result := '';
end;

function HasEntry(Path, Dir: String): Boolean;
begin
  Result := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(Path) + ';') > 0;
end;

procedure AddToPath(Dir: String);
var
  Path: String;
begin
  Path := PathEntries;
  if HasEntry(Path, Dir) then
    Exit;
  if (Path <> '') and (Copy(Path, Length(Path), 1) <> ';') then
    Path := Path + ';';
  RegWriteExpandStringValue(HKCU, 'Environment', 'Path', Path + Dir);
end;

procedure RemoveFromPath(Dir: String);
var
  Path: String;
  P: Integer;
begin
  Path := ';' + PathEntries + ';';
  P := Pos(';' + Uppercase(Dir) + ';', Uppercase(Path));
  if P = 0 then
    Exit;
  Delete(Path, P, Length(Dir) + 1);
  Path := Copy(Path, 2, Length(Path) - 2);
  RegWriteExpandStringValue(HKCU, 'Environment', 'Path', Path);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then begin
    if not FileExists(ConfigFile) then begin
      WriteConfig;
      if PhonesCheck.Checked and not WizardSilent then
        AllowPhones;
    end;
    if WizardIsTaskSelected('path') then
      AddToPath(ExpandConstant('{app}\bin'))
    else
      RemoveFromPath(ExpandConstant('{app}\bin'));
  end;
end;

procedure AskToDelete(Dir, DataDir, What: String);
begin
  if (Dir = '') or not DirExists(Dir) or (Length(Dir) <= 3) or (Pos(Uppercase(AddBackslash(DataDir)), Uppercase(Dir)) = 1) then
    Exit;
  if SuppressibleMsgBox(FmtMessage(CustomMessage('RemoveFiles'), [Dir, What]), mbConfirmation, MB_YESNO or MB_DEFBUTTON2, IDNO) = IDYES then
    DelTree(Dir, True, True, True);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  DataDir, FilesDir, BackupDir: String;
  Code: Integer;
  Firewall: Cardinal;
begin
  if CurUninstallStep <> usPostUninstall then
    Exit;
  RemoveFromPath(ExpandConstant('{app}\bin'));
  RegDeleteValue(HKCU, RunKey, 'Vitrine');
  if RegQueryDWordValue(HKCU, StateKey, 'FirewallRule', Firewall) and (Firewall = 1) and not UninstallSilent then
    ShellExec('runas', 'netsh.exe', 'advfirewall firewall delete rule name="Vitrine"', '', SW_HIDE, ewWaitUntilTerminated, Code);
  if RegQueryStringValue(HKCU, StateKey, 'DataDir', DataDir) and DirExists(DataDir) and not UninstallSilent then begin
    RegQueryStringValue(HKCU, StateKey, 'FilesDir', FilesDir);
    RegQueryStringValue(HKCU, StateKey, 'BackupDir', BackupDir);
    if SuppressibleMsgBox(FmtMessage(CustomMessage('RemoveLibrary'), [DataDir]), mbConfirmation, MB_YESNO or MB_DEFBUTTON2, IDNO) = IDYES then begin
      { Folders elsewhere (the originals, the backups) only when asked again - inside the library folder (the
        default) they go with it. }
      AskToDelete(FilesDir, DataDir, CustomMessage('RemoveFilesWhat'));
      AskToDelete(BackupDir, DataDir, CustomMessage('RemoveBackupsWhat'));
      DelTree(DataDir, True, True, True);
      DeleteFile(ConfigFile);
      RemoveDir(ExtractFileDir(ConfigFile));
      RegDeleteKeyIncludingSubkeys(HKCU, StateKey);
    end;
  end;
end;
