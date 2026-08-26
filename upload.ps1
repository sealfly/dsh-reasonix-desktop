# 用 GitHub Git Data API 推送本地提交（git push 被墙，api.github.com 可达）
# 推送 8d2ffa0..master 的 5 个提交，force 更新 master（本地链含远程链全部内容，tree 已验证一致）
$ErrorActionPreference = "Stop"
$repo = "sealfly/dsh-reasonix-desktop"
$token = (gh auth token).Trim()
$h = @{ Authorization = "Bearer $token"; Accept = "application/vnd.github+json" }

function ghpost($url, $body) {
  $json = $body | ConvertTo-Json -Depth 30 -Compress
  $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
  try {
    return Invoke-RestMethod -Uri "https://api.github.com$url" -Method Post -Headers $h -ContentType "application/json" -Body $bytes
  } catch {
    Write-Output "POST $url 失败: $($_.Exception.Message)"
    throw
  }
}
function ghget($url) {
  try {
    return Invoke-RestMethod -Uri "https://api.github.com$url" -Headers $h
  } catch {
    Write-Output "GET $url 失败: $($_.Exception.Message)"
    throw
  }
}
function ghpatch($url, $body) {
  $json = $body | ConvertTo-Json -Depth 30 -Compress
  $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
  try {
    return Invoke-RestMethod -Uri "https://api.github.com$url" -Method Patch -Headers $h -ContentType "application/json" -Body $bytes
  } catch {
    Write-Output "PATCH $url 失败: $($_.Exception.Message)"
    throw
  }
}
function Get-GitBytes($gitArgs) {
  $psi = New-Object System.Diagnostics.ProcessStartInfo
  $psi.FileName = "git"
  $psi.Arguments = ($gitArgs -join ' ')
  $psi.WorkingDirectory = "C:\Users\chenz\Desktop\dsh-reasonix-wails"
  $psi.UseShellExecute = $false
  $psi.RedirectStandardOutput = $true
  $psi.RedirectStandardError = $true
  $psi.CreateNoWindow = $true
  $p = [System.Diagnostics.Process]::Start($psi)
  $ms = New-Object System.IO.MemoryStream
  $p.StandardOutput.BaseStream.CopyTo($ms)
  $err = $p.StandardError.ReadToEnd()
  $p.WaitForExit()
  if ($p.ExitCode -ne 0) { throw "git $($psi.Arguments) failed: $err" }
  return $ms.ToArray()
}
function Get-GitText($gitArgs) {
  $bytes = Get-GitBytes $gitArgs
  return [System.Text.Encoding]::UTF8.GetString($bytes).TrimEnd("`r","`n")
}

# 1) 远程已有 blob 集合 = 本地 8d2ffa0（=远程同 commit）与 b7a3048（=远程 6cc1e558 同 tree）的 blob（内容寻址，SHA 一致）
$remoteShas = @{}
foreach ($base in @('8d2ffa079c32d96ebc47febeae9edf346b2faad0','b7a3048ef3742048e62c40022df4fa4144ebbc5b')) {
  $ls = Get-GitText "ls-tree -r $base"
  foreach ($line in ($ls -split "`n")) {
    if ($line.Trim() -eq '') { continue }
    $parts = $line -split '\t'
    $meta = $parts[0] -split '\s+'
    if ($meta[1] -eq 'blob') { $remoteShas[$meta[2]] = $true }
  }
}
Write-Output "远程已有 blob: $($remoteShas.Count)"

# 2) 本地链 blob 全集（去重）
$commitList = @(
  'bb62a3a4a3216a627390355d7af4a33d43e6293f',
  'ff43b227ae95a16905f4b2e00a075d497109971d',
  '9baf05b583080a39068c2e90bd60fef12f5aea44',
  'b7a3048ef3742048e62c40022df4fa4144ebbc5b',
  'a69eaa26f34d51f800e6bcddbc986b6d1a76eecb'
)
$localBlobs = @{}
foreach ($c in $commitList) {
  $ls = Get-GitText "ls-tree -r $c"
  foreach ($line in ($ls -split "`n")) {
    if ($line.Trim() -eq '') { continue }
    $parts = $line -split '\t'
    $sha = ($parts[0] -split '\s+')[2]
    $localBlobs[$sha] = $true
  }
}
Write-Output "本地 blob 总数: $($localBlobs.Count)"

# 3) 上传缺失 blob
$missing = @($localBlobs.Keys | Where-Object { -not $remoteShas.ContainsKey($_) })
Write-Output "需上传 blob: $($missing.Count)"
$n = 0
foreach ($s in $missing) {
  $n++
  $bytes = Get-GitBytes "cat-file blob $s"
  $b64 = [Convert]::ToBase64String($bytes)
  $r = ghpost "/repos/$repo/git/blobs" @{ content = $b64; encoding = "base64" }
  if ($r.sha -ne $s) { throw "blob SHA mismatch: expected $s got $($r.sha)" }
  Write-Output "  [$n/$($missing.Count)] blob $($s.Substring(0,10)) $($bytes.Length) bytes OK"
}

# 4) 递归重建 tree
function Build-TreeFromEntries($entries, $prefix) {
  # entries: array of hashtables {path, sha, mode, type}
  $dirs = @{}
  $files = @()
  foreach ($e in $entries) {
    $rel = $e.path
    if ($prefix.Length -gt 0) {
      if (-not $rel.StartsWith($prefix)) { continue }
      $rel = $rel.Substring($prefix.Length)
    }
    $rel = $rel.TrimStart('/')
    if ($rel -match '/') {
      $d = ($rel -split '/')[0]
      if (-not $dirs.ContainsKey($d)) { $dirs[$d] = @() }
      $dirs[$d] += $e
    } else {
      $files += $e
    }
  }
  $items = @()
  foreach ($f in $files) {
    $name = $f.path
    if ($prefix.Length -gt 0) { $name = $name.Substring($prefix.Length).TrimStart('/') }
    $items += @{ path = $name; mode = $f.mode; type = 'blob'; sha = $f.sha }
  }
  foreach ($d in ($dirs.Keys | Sort-Object)) {
    $childPrefix = $prefix + $d + '/'
    $childSha = Build-TreeFromEntries $dirs[$d] $childPrefix
    $items += @{ path = $d; mode = '040000'; type = 'tree'; sha = $childSha }
  }
  if ($items.Count -eq 0) { return $null }
  $r = ghpost "/repos/$repo/git/trees" @{ tree = $items }
  return $r.sha
}

# 5) 逐提交创建 tree + commit
$parentSha = '8d2ffa079c32d96ebc47febeae9edf346b2faad0'
foreach ($c in $commitList) {
  Write-Output "--- 提交 $($c.Substring(0,8))"
  # 该提交的 entries
  $ls = Get-GitText "ls-tree -r $c"
  $entries = @()
  foreach ($line in ($ls -split "`n")) {
    if ($line.Trim() -eq '') { continue }
    $parts = $line -split '\t'
    $meta = $parts[0] -split '\s+'
    $entries += @{ path = $parts[1]; sha = $meta[2]; mode = $meta[0]; type = $meta[1] }
  }
  $rootTree = Build-TreeFromEntries $entries ""
  Write-Output "  根 tree: $rootTree"
  $msg = Get-GitText "log -1 --format=%B $c"
  $an = Get-GitText "log -1 --format=%an $c"; $ae = Get-GitText "log -1 --format=%ae $c"; $ad = Get-GitText "log -1 --format=%aI $c"
  $cn = Get-GitText "log -1 --format=%cn $c"; $ce = Get-GitText "log -1 --format=%ce $c"; $cd = Get-GitText "log -1 --format=%cI $c"
  $body = @{
    message = $msg
    tree = $rootTree
    parents = @($parentSha)
    author = @{ name = $an; email = $ae; date = $ad }
    committer = @{ name = $cn; email = $ce; date = $cd }
  }
  $commit = ghpost "/repos/$repo/git/commits" $body
  Write-Output "  新 commit: $($commit.sha)"
  $parentSha = $commit.sha
}

# 6) force 更新 master
$ref = ghpatch "/repos/$repo/git/refs/heads/master" @{ sha = $parentSha; force = $true }
Write-Output "master 已更新: $($ref.object.sha)"
Write-Output "完成"
