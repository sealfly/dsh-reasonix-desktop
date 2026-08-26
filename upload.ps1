# upload.ps1 — GitHub Git Data API 推送通道（git push 被墙时的替代）。
# 改造版：基于远程当前 master 动态推送 —— 找出本地"内容不在远程"的提交，
# 重建为挂在远程 head 之后的新提交链，快进更新 master（不覆盖远程历史）。
# 用法：pwsh -File upload.ps1

$ErrorActionPreference = "Stop"
$repo = "sealfly/dsh-reasonix-desktop"
# 仓库根 = 脚本所在目录（可移植：clone 到任意路径都可运行，无需改路径）
$workdir = $PSScriptRoot
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
  $psi.WorkingDirectory = $workdir
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

# ===== 1) 远程当前 master =====
$ref = ghget "/repos/$repo/git/ref/heads/master"
$remoteHead = $ref.object.sha
Write-Output "远程 master: $remoteHead"

# ===== 2) 远程已有对象集合（内容寻址判据）=====
$rc = ghget "/repos/$repo/commits/$remoteHead"
$remoteTrees = @{}
$remoteBlobs = @{}
# 远程 master tree 递归（当前工作树的所有 blob/tree）
$rt = ghget "/repos/$repo/git/trees/$($rc.commit.tree.sha)?recursive=1"
foreach ($e in $rt.tree) {
  if ($e.type -eq 'blob') { $remoteBlobs[$e.sha] = $true }
  elseif ($e.type -eq 'tree') { $remoteTrees[$e.sha] = $true }
}
# 远程链所有提交的顶层 tree（历史 tree 也视为已有——内容寻址判 base 用）
$chain = ghget "/repos/$repo/commits?per_page=100&sha=master"
foreach ($citem in $chain) { $remoteTrees[$citem.commit.tree.sha] = $true }
Write-Output "远程已有: blob $($remoteBlobs.Count) tree $($remoteTrees.Count)"

# ===== 3) 本地提交列表，找"内容已在远程"的 base =====
$commits = (Get-GitText "log --format=%H master") -split "`n" | Where-Object { $_.Trim() -ne '' }
$toPush = @()
$base = $null
foreach ($c in $commits) {
  $treeSha = (Get-GitText "rev-parse ${c}^{tree}").Trim()
  if ($remoteTrees.ContainsKey($treeSha)) {
    $base = $c
    break
  }
  $toPush = @($c) + $toPush
}
if (-not $base) { throw "未找到内容已在远程的基础提交（远程 head 的 tree 不覆盖任何本地提交）" }
Write-Output "base（内容已在远程）: $($base.Substring(0,10))"
Write-Output "待推送提交（旧→新）: $(($toPush | ForEach-Object { $_.Substring(0,10) }) -join ' ')"

# ===== 4) 上传缺失 blob =====
# 本地 base 的 blob 视为远程已有（内容寻址：base tree 在远程）；待推提交的 blob 全集减之
$baseBlobs = @{}
foreach ($line in ((Get-GitText "ls-tree -r $base") -split "`n")) {
  if ($line.Trim() -eq '') { continue }
  $meta = ($line -split "`t")[0] -split '\s+'
  if ($meta[1] -eq 'blob') { $remoteBlobs[$meta[2]] = $true; $baseBlobs[$meta[2]] = $true }
}
$allBlobs = @{}
foreach ($c in $toPush) {
  foreach ($line in ((Get-GitText "ls-tree -r $c") -split "`n")) {
    if ($line.Trim() -eq '') { continue }
    $meta = ($line -split "`t")[0] -split '\s+'
    if ($meta[1] -eq 'blob') { $allBlobs[$meta[2]] = $true }
  }
}
$missing = @($allBlobs.Keys | Where-Object { -not $remoteBlobs.ContainsKey($_) })
Write-Output "缺失 blob: $($missing.Count)"
foreach ($s in $missing) {
  $b64 = [Convert]::ToBase64String((Get-GitBytes "cat-file blob $s"))
  ghpost "/repos/$repo/git/blobs" @{ content = $b64; encoding = "base64" } | Out-Null
  Write-Output "  [blob] $($s.Substring(0,10)) OK"
}

# ===== 5) 递归重建 tree（远程/已创建命中则跳过）=====
$createdTrees = @{}
function Build-Tree($commit, $dir) {
  if ($dir -eq '') {
    $shaLine = (Get-GitText "rev-parse ${commit}^{tree}").Trim()
  } else {
    $shaLine = (Get-GitText "rev-parse ${commit}:${dir}").Trim()
  }
  if ($remoteTrees.ContainsKey($shaLine) -or $createdTrees.ContainsKey($shaLine)) {
    return $shaLine
  }
  $list = if ($dir -eq '') { Get-GitText "ls-tree $commit" } else { Get-GitText "ls-tree ${commit}:${dir}" }
  $entries = @()
  foreach ($line in ($list -split "`n")) {
    if ($line.Trim() -eq '') { continue }
    $parts = $line -split "`t"
    $meta = $parts[0] -split '\s+'
    $name = $parts[1]
    if ($meta[1] -eq 'tree') {
      $subDir = if ($dir -eq '') { $name } else { "$dir/$name" }
      $sub = Build-Tree $commit $subDir
      $entries += @{ path = $name; mode = $meta[0]; type = "tree"; sha = $sub }
    } else {
      $entries += @{ path = $name; mode = $meta[0]; type = $meta[1]; sha = $meta[2] }
    }
  }
  $t = ghpost "/repos/$repo/git/trees" @{ tree = $entries }
  $createdTrees[$t.sha] = $true
  return $t.sha
}

# ===== 6) 逐提交创建 commit（parent 链挂到远程 head 之后）=====
$prev = $remoteHead
foreach ($c in $toPush) {
  $treeSha = Build-Tree $c ''
  $msg = (Get-GitText "log -1 --format=%B $c").Trim()
  $au = (Get-GitText "log -1 --format=%an|%ae|%aI $c").Split('|')
  $co = (Get-GitText "log -1 --format=%cn|%ce|%cI $c").Split('|')
  $body = @{
    message   = $msg
    tree      = $treeSha
    parents   = @($prev)
    author    = @{ name = $au[0]; email = $au[1]; date = $au[2] }
    committer = @{ name = $co[0]; email = $co[1]; date = $co[2] }
  }
  $nc = ghpost "/repos/$repo/git/commits" $body
  Write-Output "提交 $($c.Substring(0,10)) -> $($nc.sha.Substring(0,10))"
  $prev = $nc.sha
}

# ===== 7) 快进更新 master =====
ghpatch "/repos/$repo/git/refs/heads/master" @{ sha = $prev; force = $true } | Out-Null
Write-Output "master 已更新: $prev"
Write-Output "完成"
