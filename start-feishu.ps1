param(
    [string]$Address = ":8080"
)

$ErrorActionPreference = "Stop"

$requiredVariables = @(
    "FEISHU_APP_ID",
    "FEISHU_APP_SECRET",
    "FEISHU_REDIRECT_URL"
)

foreach ($variableName in $requiredVariables) {
    $value = [Environment]::GetEnvironmentVariable($variableName, "Process")
    if ([string]::IsNullOrWhiteSpace($value)) {
        throw "Missing required environment variable: $variableName"
    }
}

if ([string]::IsNullOrWhiteSpace($env:GOCACHE)) {
    $env:GOCACHE = Join-Path $PSScriptRoot ".codex_tmp\go-cache"
}

Set-Location -LiteralPath $PSScriptRoot
go run ./backend/cmd/chirepk -addr $Address
