# ===========================================================================
#  start-multi.ps1 - Multi-Account Launcher for rozclub-go
# ===========================================================================
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ScriptDir

function Get-BotConfigs {
    $files = Get-ChildItem -Path $ScriptDir -Filter "config*.json" | Where-Object { $_.Name -ne "trade_config.json" } | Sort-Object Name
    return $files
}

function Launch-BotInstance {
    param([string]$ConfigFile)
    $fileName = Split-Path -Leaf $ConfigFile
    $title = "ROZCLUB-GO [$fileName]"
    Write-Host "  [*] กำลังสตาร์ต $title..." -ForegroundColor Green
    Start-Process -FilePath "conhost.exe" -ArgumentList "title $title & `"$ScriptDir\rozclub-go.exe`" -c `"$fileName`"" -WorkingDirectory $ScriptDir
}

function Stop-AllBots {
    Write-Host "`n  [*] กำลังปิด rozclub-go ทั้งหมด..." -ForegroundColor Yellow
    Get-Process -Name "rozclub-go" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Write-Host "  [*] ปิดโปรเซสบอททั้งหมดเรียบร้อยแล้วค่ะ ✨" -ForegroundColor Green
    Start-Sleep -Seconds 1
}

do {
    Clear-Host
    Write-Host "============================================================" -ForegroundColor Green
    Write-Host "       Rozclub-Go Standalone Multi-Account Launcher         " -ForegroundColor Cyan
    Write-Host "============================================================" -ForegroundColor Green
    Write-Host ""

    $runningBots = Get-Process -Name "rozclub-go" -ErrorAction SilentlyContinue
    $count = if ($runningBots) { @($runningBots).Count } else { 0 }
    Write-Host "  [สถานะระบบ] บอท rozclub-go ที่กำลังทำงานอยู่: $count จอ" -ForegroundColor Yellow
    Write-Host ""

    $configs = Get-BotConfigs
    Write-Host "  รายชื่อไฟล์ Config ที่พบ:" -ForegroundColor Cyan
    if ($configs.Count -eq 0) {
        Write-Host "    (ไม่พบไฟล์ config*.json ในโฟลเดอร์)" -ForegroundColor Gray
    } else {
        for ($i = 0; $i -lt $configs.Count; $i++) {
            $cfg = $configs[$i]
            $hint = ""
            try {
                $json = Get-Content $cfg.FullName -Raw | ConvertFrom-Json
                $hint = " [User: $($json.username), Char: $($json.char_slot), Map: $($json.lock_map)]"
            } catch {}
            Write-Host "    [$($i + 1)] $($cfg.Name)$hint" -ForegroundColor White
        }
    }

    Write-Host ""
    Write-Host "  ตัวเลือก:" -ForegroundColor DarkYellow
    Write-Host "    [A] เปิดทุก Config พร้อมกันทั้งหมด (Launch All)" -ForegroundColor Green
    Write-Host "    [K] ปิดบอททั้งหมด (Kill All rozclub-go)" -ForegroundColor Red
    Write-Host "    [Q] ออกจากเมนู (บอทที่เปิดอยู่ยังทำงานต่อ)" -ForegroundColor DarkGray
    Write-Host ""

    $choice = Read-Host "  เลือกหมายเลข Config หรือคำสั่งที่ต้องการ"
    if ([string]::IsNullOrWhiteSpace($choice)) { continue }
    $choice = $choice.Trim()

    if ($choice -eq "q" -or $choice -eq "Q") {
        Write-Host "`n  [*] ออกจากโปรแกรมเรียบร้อยค่ะ บอทยังคงรันตามปกติ ✨`n" -ForegroundColor Green
        break
    }

    if ($choice -eq "k" -or $choice -eq "K") {
        Stop-AllBots
        continue
    }

    if ($choice -eq "a" -or $choice -eq "A") {
        Write-Host "`n  [*] กำลังเปิดบอททุกไอดี..." -ForegroundColor Green
        foreach ($cfg in $configs) {
            Launch-BotInstance -ConfigFile $cfg.FullName
            Start-Sleep -Milliseconds 600
        }
        Write-Host "  [*] เปิดครบทุกไอดีแล้วค่ะ ✨`n" -ForegroundColor Green
        Start-Sleep -Seconds 2
        continue
    }

    if ($choice -match '^\d+$') {
        $idx = [int]$choice - 1
        if ($idx -ge 0 -and $idx -lt $configs.Count) {
            $selected = $configs[$idx]
            Launch-BotInstance -ConfigFile $selected.FullName
            Start-Sleep -Seconds 1
        } else {
            Write-Host "  [!] ไม่พบหมายเลข Config ที่เลือก" -ForegroundColor Red
            Start-Sleep -Seconds 1
        }
    }
} while ($true)
