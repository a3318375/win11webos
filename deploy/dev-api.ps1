# Local development only: emulate the container /data mount.
$env:CLOUD_DEV_DATA_DIR = 'D:\tmp\res\data'
$env:CLOUD_LOCAL_ROOTS = $env:CLOUD_DEV_DATA_DIR
$env:CLOUD_DATA_DIR = Join-Path $PSScriptRoot '../api/data'
Set-Location (Join-Path $PSScriptRoot '../api')
go run -buildvcs=false ./cmd/server
