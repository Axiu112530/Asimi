param([string]$Audit = 'build/bin/v021-licenses-audit')

$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
. "$Repo/validation/phase0/env.ps1"
$Notice = Join-Path $Repo 'docs/0.2.1/THIRD-PARTY-NOTICES.txt'
$Sections = [Collections.Generic.List[string]]::new()
$Sections.Add("Third-party notices for the Asimi Preset 0.2.1 Windows x64 candidate.`nThese notices cover bundled dependencies. The application license remains an owner decision.")
$LicensePattern = '^(LICEN[CS]E|COPYING|NOTICE|UNLICENSE|PATENTS)'
Push-Location $Repo
try {
    $GoModules = @(go list -deps -f '{{with .Module}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}' . | Where-Object { $_ -and $_ -notlike 'asimi|*' } | Sort-Object -Unique)
    if ($LASTEXITCODE -or !$GoModules.Count) { throw 'Go dependency discovery failed' }
    $GoVersion = go env GOVERSION
    $GoRoot = go env GOROOT
    $GoModules += "Go standard library|$GoVersion|$GoRoot"
    foreach ($Module in $GoModules) {
        $Name,$Version,$Directory = $Module -split '\|',3
        $Files = @(Get-ChildItem -LiteralPath $Directory -File | Where-Object Name -Match $LicensePattern | Sort-Object Name)
        if (!$Files.Count) { throw "No license found for $Name" }
        foreach ($File in $Files) {
            $Sections.Add("Go: $Name $Version ($($File.Name))`n`n$([IO.File]::ReadAllText($File.FullName).Trim())")
        }
    }
    $Packages = @(
        Get-ChildItem -LiteralPath (Join-Path $Repo "$Audit/assets") -Filter '*.js.map' | ForEach-Object {
            (Get-Content -Raw -LiteralPath $_.FullName | ConvertFrom-Json).sources | ForEach-Object {
                if ($_ -match 'node_modules/((?:@[^/]+/)?[^/]+)') { $Matches[1] }
            }
        } | Sort-Object -Unique
    )
    if (!$Packages.Count) { throw 'Production sourcemap package inventory unavailable' }
    foreach ($Name in $Packages) {
        $Directory = Join-Path $Repo "frontend/node_modules/$Name"
        $Package = Get-Content -Raw -LiteralPath (Join-Path $Directory 'package.json') | ConvertFrom-Json
        $Files = @(Get-ChildItem -LiteralPath $Directory -File | Where-Object Name -Match $LicensePattern | Sort-Object Name)
        if (!$Files.Count -and $Name -eq '@vue/devtools-api' -and $Package.version -eq '6.6.4' -and $Package.license -eq 'MIT') {
            # npm tarball omits the upstream notice. The tagged source confirms the MIT terms and copyright.
            $MIT = [IO.File]::ReadAllText((Join-Path $Repo 'frontend/node_modules/vue/LICENSE')).Trim()
            $MIT = $MIT -replace '(?m)^Copyright \(c\) .*$', 'Copyright (c) 2014-present Evan You'
            $Sections.Add("npm: $Name $($Package.version) (MIT)`nhttps://github.com/vuejs/devtools-v6/blob/v6.6.4/LICENSE`n`n$MIT")
            continue
        }
        if (!$Files.Count) { throw "No license found for $Name" }
        foreach ($File in $Files) {
            $Sections.Add("npm: $Name $($Package.version) ($($File.Name))`n`n$([IO.File]::ReadAllText($File.FullName).Trim())")
        }
    }
    [IO.File]::WriteAllText($Notice,($Sections -join "`n`n$('=' * 72)`n`n")+"`n",[Text.UTF8Encoding]::new($false))
} finally { Pop-Location }
