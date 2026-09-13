function Split-GoFile {
param(
    [Parameter(Mandatory=$true)][string]$Path,
    [Parameter(Mandatory=$true)][scriptblock]$BucketOf  # name -> bucket
)
$gi = 'C:\Users\16143\go\bin\goimports.exe'
$dir = Split-Path $Path
$file = Split-Path $Path -Leaf
$base = $file -replace '\.go$',''
Set-Location $dir
$lines = Get-Content $file
$headerEnd = 0
for ($j = 0; $j -lt $lines.Count; $j++) { if ($lines[$j] -match '^\)') { $headerEnd = $j + 1; break } }
if ($headerEnd -eq 0) { throw 'import block end not found' }
$buckets = @{}
$mainList = [System.Collections.Generic.List[string]]::new()
$mainList.AddRange([string[]]$lines[0..($headerEnd-1)])
$i = $headerEnd
while ($i -lt $lines.Count) {
    $start = $i
    while ($i -lt $lines.Count -and $lines[$i] -match '^\s*(//|$)') { $i++ }
    if ($i -ge $lines.Count) { break }
    if ($lines[$i] -notmatch '^(func|type|var|const)') { $i++; continue }
    $decl = $lines[$i]; $i++
    while ($i -lt $lines.Count -and $lines[$i] -notmatch '^(func |type |var |const |// [A-Z(])') { $i++ }
    $block = @($lines[$start..($i-1)])
    $name = ''
    if ($decl -match '^(?:func|type|var|const)(?:\s+\([^)]*\))?\s+([A-Za-z_][A-Za-z0-9_]*)') { $name = $Matches[1] }
    elseif ($decl -match '^func\s+\(([^)]*)\)\s+([A-Za-z_][A-Za-z0-9_]*)') { $name = $Matches[2] }
    $b = & $BucketOf $name $decl
    if ($b -eq 'main') { $mainList.AddRange([string[]]$block); $mainList.Add('') }
    else {
        if (-not $buckets[$b]) { $buckets[$b] = [System.Collections.Generic.List[string]]::new() }
        $buckets[$b].AddRange([string[]]$block); $buckets[$b].Add('')
    }
}
[IO.File]::WriteAllLines((Join-Path $dir $file), $mainList)
foreach ($t in $buckets.GetEnumerator()) {
    $out = [System.Collections.Generic.List[string]]::new()
    $out.Add('package ' + ($file -replace '\..*','' -replace '_.*',''))
    # 从原 package 行取真实包名
    $pkg = ($lines | Select-String '^package ' | Select-Object -First 1).Line -replace 'package ',''
    $out[0] = "package $pkg"
    $out.Add('')
    $out.AddRange([string[]]$t.Value)
    [IO.File]::WriteAllLines((Join-Path $dir "$base$($t.Key).go"), $out)
}
& $gi -w (Join-Path $dir $file)
foreach ($t in $buckets.Keys) { & $gi -w (Join-Path $dir "$base$($t).go") }
Write-Output "split done: $($buckets.Keys -join ', ')"
