$gh = "C:\Users\ROG Zephyrus G16\Desktop\DSH\dsh-reasonix\.tools\bin\gh.exe"
$r = "C:\Users\ROG Zephyrus G16\Desktop\DSH\dsh-reasonix\release"
Set-Location "C:\Users\ROG Zephyrus G16\Desktop\DSH\dsh-reasonix"

$assets = @(
  "DSH-Reasonix Setup 0.1.0.exe",
  "DSH-Reasonix-0.1.0-win.zip",
  "DSH-Reasonix-Setup.exe"
)
foreach ($a in $assets) {
  $p = Join-Path $r $a
  if (Test-Path $p) {
    Write-Host ("上传 {0} ({1:N1} MB)..." -f $a, ((Get-Item $p).Length/1MB))
    & $gh release upload v0.1.0 $p --repo sealfly/dsh-Reasonix-desktop --clobber 2>&1 | Out-String
  }
}
Write-Host "全部上传完成"
