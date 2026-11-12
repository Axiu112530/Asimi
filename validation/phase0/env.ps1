# Dot-source in a dedicated child PowerShell. All environment changes are local
# to that process; this never writes user/machine environment or proxy settings.
$Phase0Root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$env:PATH = (Join-Path $Phase0Root '.phase0-tools/go/bin') + ';' + $env:PATH
$env:GOPATH = Join-Path $Phase0Root '.phase0-cache/gopath'
$env:GOMODCACHE = Join-Path $Phase0Root '.phase0-cache/gomod'
$env:GOCACHE = Join-Path $Phase0Root '.phase0-cache/go-build'
$env:GOBIN = Join-Path $Phase0Root '.phase0-tools/bin'
$env:GOENV = 'off'
$env:GOTOOLCHAIN = 'local'
$env:CGO_ENABLED = '0'
$env:npm_config_cache = Join-Path $Phase0Root '.phase0-cache/npm'
