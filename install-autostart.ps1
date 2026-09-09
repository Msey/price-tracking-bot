# Builds the exe and creates Desktop + Startup shortcuts (same idea as kupim bot).

$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot
if (-not $Root) { $Root = Split-Path -Parent $MyInvocation.PSCommandPath }

$exe = Join-Path $Root "price-tracking-bot.exe"
$vbs = Join-Path $Root "start.vbs"
$envFile = Join-Path $Root ".env"

if (-not (Test-Path -LiteralPath $envFile)) {
  throw "Missing .env. Copy .env.example and set BOT_TOKEN."
}
if (-not (Test-Path -LiteralPath $vbs)) {
  throw "Missing start.vbs"
}

$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $go) {
  throw "Go is not on PATH."
}

Write-Host "Building price-tracking-bot.exe..."
$env:GOTOOLCHAIN = "local"
$env:CGO_ENABLED = "0"
Push-Location $Root
try {
  & go build -o $exe ./cmd/bot
  if ($LASTEXITCODE -ne 0) { throw "go build failed: $LASTEXITCODE" }
}
finally {
  Pop-Location
}

function New-BotShortcut {
  param(
    [Parameter(Mandatory = $true)][string]$LnkPath,
    [Parameter(Mandatory = $true)][string]$Description
  )
  $wscript = Join-Path $env:SystemRoot "System32\wscript.exe"
  $shell = New-Object -ComObject WScript.Shell
  try {
    $shortcut = $shell.CreateShortcut($LnkPath)
    $shortcut.TargetPath = $wscript
    $shortcut.Arguments = '"' + $vbs + '"'
    $shortcut.WorkingDirectory = $Root
    $shortcut.WindowStyle = 7
    $shortcut.Description = $Description
    if (Test-Path -LiteralPath $exe) {
      $shortcut.IconLocation = $exe + ",0"
    }
    $shortcut.Save()
  }
  finally {
    [void][System.Runtime.InteropServices.Marshal]::ReleaseComObject($shell)
  }
}

# "Трекинг цен.lnk" without putting UTF-8 in the script file (PS 5.1).
$ruName = -join @(
  [char]0x0422, [char]0x0440, [char]0x0435, [char]0x043A, [char]0x0438, [char]0x043D, [char]0x0433,
  ' ',
  [char]0x0446, [char]0x0435, [char]0x043D
) + ".lnk"

$desktop = [Environment]::GetFolderPath("Desktop")
$startup = [Environment]::GetFolderPath("Startup")
if ($desktop) {
  New-BotShortcut -LnkPath (Join-Path $desktop $ruName) -Description "DNS price tracking bot"
  Write-Host "Desktop shortcut: $ruName"
}
if ($startup) {
  New-BotShortcut -LnkPath (Join-Path $startup $ruName) -Description "DNS price tracking bot autostart"
  Write-Host "Startup folder: $startup"
}

Write-Host "Done. Start via the shortcut or start.vbs"
