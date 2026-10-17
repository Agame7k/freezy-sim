@echo off
rem Builds freezy-sim.exe in this folder (double-click it, or run "build" here).
rem Plain "go build" does not work in this folder: the program is in cmd\freezy-sim.
rem A running freezy-sim.exe is moved aside to freezy-sim.old.exe first (Windows will not overwrite a running exe);
rem restart freezy-sim afterwards to run the new build.
cd /d "%~dp0"
where go >nul 2>&1 || (echo Go is not installed or not on PATH: https://go.dev/dl/ & pause & exit /b 1)
if exist freezy-sim.exe (
  if exist freezy-sim.old.exe del freezy-sim.old.exe >nul 2>&1
  move /y freezy-sim.exe freezy-sim.old.exe >nul 2>&1 || (echo Could not move freezy-sim.exe aside. Close freezy-sim and try again. & pause & exit /b 1)
)
go build -o freezy-sim.exe ./cmd/freezy-sim
if errorlevel 1 (
  if not exist freezy-sim.exe if exist freezy-sim.old.exe move /y freezy-sim.old.exe freezy-sim.exe >nul
  echo Build failed.
  pause
  exit /b 1
)
echo Built freezy-sim.exe. Restart freezy-sim if it is running.
