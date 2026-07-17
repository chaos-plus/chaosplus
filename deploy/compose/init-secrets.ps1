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

function Set-EnvValue([string]$Key, [string]$Value) {
    $lines = [Collections.Generic.List[string]](Get-Content -LiteralPath $envFile)
    $found = $false
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -match "^$([regex]::Escape($Key))=") {
            $lines[$i] = "$Key=$Value"
            $found = $true
            break
        }
    }
    if (-not $found) { $lines.Add("$Key=$Value") }
    [IO.File]::WriteAllLines($envFile, $lines)
}

function Write-Secret([string]$Name, [string]$Value) {
    [IO.File]::WriteAllText((Join-Path $secrets $Name), $Value)
}

$postgres = New-RandomValue
$zitadelDB = New-RandomValue
$spicedbDB = New-RandomValue
$migrator = New-RandomValue
$runtime = New-RandomValue
$redis = New-RandomValue
$spicedbToken = New-RandomValue 32
$admin = "Cp!$(New-RandomValue 16)"
$masterKey = New-RandomValue 16
$sessionKey = New-RandomValue 16
$expiration = (Get-Date).ToUniversalTime().AddYears(1).ToString('yyyy-MM-ddTHH:mm:ssZ')

@{
    POSTGRES_ADMIN_PASSWORD = $postgres
    ZITADEL_DB_PASSWORD = $zitadelDB
    SPICEDB_DB_PASSWORD = $spicedbDB
    CHAOSPLUS_MIGRATOR_PASSWORD = $migrator
    CHAOSPLUS_RUNTIME_PASSWORD = $runtime
    REDIS_PASSWORD = $redis
    SPICEDB_TOKEN = $spicedbToken
    ZITADEL_FIRST_ADMIN_PASSWORD = $admin
    ZITADEL_MACHINE_KEY_EXPIRATION = $expiration
    ZITADEL_LOGIN_PAT_EXPIRATION = $expiration
}.GetEnumerator() | ForEach-Object { Set-EnvValue $_.Key $_.Value }

Write-Secret 'zitadel_masterkey' $masterKey
Write-Secret 'redis_password' $redis
Write-Secret 'spicedb_token' $spicedbToken
Write-Secret 'session_encryption_key' $sessionKey
Write-Secret 'chaosplus_migration_dsn' "postgres://chaosplus_migrator:$migrator@postgres:5432/chaosplus?sslmode=disable"
Write-Secret 'chaosplus_runtime_dsn' "postgres://chaosplus_app:$runtime@postgres:5432/chaosplus?sslmode=disable"

Write-Host "Generated $envFile and Docker secret files."
Write-Host "Initial login: admin@chaosplus.local"
Write-Host "Initial password: $admin"
