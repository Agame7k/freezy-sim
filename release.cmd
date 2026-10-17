@echo off
rem Builds a release zip: release 0.1.0
rem Writes dist\freezy-sim-v<version>-windows-amd64.zip (the exe, example config, scenarios, docs, CAD tool and
rem licenses) and a .sha256 file beside it. Run it from a clean checkout of the tag you are releasing.
setlocal
cd /d "%~dp0"
set VERSION=%~1
if "%VERSION%"=="" (echo Usage: release ^<version^>   for example: release 0.1.0 & exit /b 1)
where go >nul 2>&1 || (echo Go is not installed or not on PATH: https://go.dev/dl/ & exit /b 1)

set NAME=freezy-sim-v%VERSION%-windows-amd64
set STAGE=dist\%NAME%
if exist "%STAGE%" rmdir /s /q "%STAGE%"
if exist "dist\%NAME%.zip" del "dist\%NAME%.zip"
mkdir "%STAGE%" || exit /b 1

echo Checking formatting, vet and tests...
for /f %%f in ('gofmt -l internal cmd web') do (echo Not gofmt-formatted: %%f & exit /b 1)
go vet ./... || exit /b 1
go test ./... || exit /b 1

echo Building freezy-sim.exe %VERSION%...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
go build -trimpath -ldflags "-s -w -X github.com/Agame7k/freezy-sim/internal/version.Version=%VERSION%" -o "%STAGE%\freezy-sim.exe" ./cmd/freezy-sim || exit /b 1

copy /y README.md "%STAGE%\" >nul
copy /y LICENSE "%STAGE%\LICENSE.txt" >nul
copy /y THIRD_PARTY_NOTICES.md "%STAGE%\" >nul
copy /y CHANGELOG.md "%STAGE%\" >nul
copy /y config.example.json "%STAGE%\" >nul
robocopy docs "%STAGE%\docs" /e /njh /njs /nfl /ndl >nul
robocopy scenarios "%STAGE%\scenarios" *.json /njh /njs /nfl /ndl >nul
robocopy tools\cad "%STAGE%\tools\cad" export_field.py /njh /njs /nfl /ndl >nul
rem FIRST's field CAD is never shipped (docs/FIELD_MODEL.md): it is not embedded (web/assets.go) and not copied here.

rem Windows' tar.exe (bsdtar, Windows 10 1803+) writes standard "/" zip paths; Compress-Archive and .NET's ZipFile in
rem Windows PowerShell write "\", which other systems unpack wrongly. The zip holds one folder named after the release.
rem Call it by full path: Git for Windows (and GitHub's runners) put GNU tar on PATH, which can't write zips.
set TAR=%SystemRoot%\System32\tar.exe
if not exist "%TAR%" (echo %TAR% not found: it ships with Windows 10 1803 and later & exit /b 1)
"%TAR%" -a -c -f "dist\%NAME%.zip" -C dist "%NAME%" || exit /b 1
powershell -NoProfile -Command "$h = (Get-FileHash 'dist\%NAME%.zip' -Algorithm SHA256).Hash.ToLower(); Set-Content -Encoding ascii 'dist\%NAME%.zip.sha256' ($h + '  %NAME%.zip')" || exit /b 1
"%STAGE%\freezy-sim.exe" -version
echo Wrote dist\%NAME%.zip
type "dist\%NAME%.zip.sha256"
