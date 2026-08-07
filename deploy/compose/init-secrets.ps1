[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$envFile = Join-Path $root '.env'
$example = Join-Path $root '.env.example'
$secrets = Join-Path $root 'secrets'

if (-not (Test-Path -LiteralPath $envFile)) {
    Copy-Item -LiteralPath $example -Destination $envFile
}
New-Item -ItemType Directory -Path $secrets -Force | Out-Null

function New-RandomValue([int]$Bytes = 24) {
    $data = [byte[]]::new($Bytes)
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($data) } finally { $rng.Dispose() }
    return ([BitConverter]::ToString($data) -replace '-', '').ToLowerInvariant()
}

function New-RandomKeyBase64 {
    $data = [byte[]]::new(32)
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($data) } finally { $rng.Dispose() }
    return [Convert]::ToBase64String($data)
}

function Set-EnvValue([string]$Key, [string]$Value) {
    $lines = [Collections.Generic.List[string]](Get-Content -LiteralPath $envFile)
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -match "^$([regex]::Escape($Key))=") {
            $lines[$i] = "$Key=$Value"
            [IO.File]::WriteAllLines($envFile, $lines)
            return
        }
    }
    $lines.Add("$Key=$Value")
    [IO.File]::WriteAllLines($envFile, $lines)
}

function Write-Secret([string]$Name, [string]$Value) {
    [IO.File]::WriteAllText((Join-Path $secrets $Name), $Value)
}

$postgres = New-RandomValue
$migrator = New-RandomValue
$runtime = New-RandomValue
$redis = New-RandomValue
$admin = "Cp!$(New-RandomValue 16)"

Set-EnvValue POSTGRES_ADMIN_PASSWORD $postgres
Set-EnvValue CHAOSPLUS_MIGRATOR_PASSWORD $migrator
Set-EnvValue CHAOSPLUS_RUNTIME_PASSWORD $runtime
Set-EnvValue REDIS_PASSWORD $redis

Write-Secret redis_password $redis
Write-Secret authn_signing_key (New-RandomKeyBase64)
Write-Secret authn_mfa_key (New-RandomKeyBase64)
Write-Secret initial_admin_password $admin
Write-Secret chaosplus_migration_dsn "postgres://chaosplus_migrator:$migrator@postgres:5432/chaosplus?sslmode=disable"
Write-Secret chaosplus_runtime_dsn "postgres://chaosplus_app:$runtime@postgres:5432/chaosplus?sslmode=disable"

Write-Host "Generated $envFile and Docker secret files."
Write-Host "Initial login: admin@chaosplus.local"
Write-Host "Initial password: $admin"
