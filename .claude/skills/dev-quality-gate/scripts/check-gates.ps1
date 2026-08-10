[CmdletBinding()]
param(
    [ValidateSet('auto', 'backend', 'frontend', 'docs', 'all')]
    [string]$Scope = 'auto',
    [switch]$Full,
    [string[]]$ChangedPath
)

$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion.Major -ge 7) { $PSNativeCommandUseErrorActionPreference = $false }
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..\..\..')).Path
$skillRuntime = Join-Path $PSScriptRoot 'skill-runtime.py'
$failures = [Collections.Generic.List[string]]::new()

function Invoke-NativeStep([string]$Label, [string]$WorkingDirectory, [string]$Command, [string[]]$Arguments) {
    Write-Host "`n== $Label ==" -ForegroundColor Cyan
    Push-Location $WorkingDirectory
    $previousErrorAction = $ErrorActionPreference
    try {
        # Windows PowerShell 5.1 promotes native stderr to terminating errors under
        # $ErrorActionPreference='Stop', which misreports successful tools that write
        # progress to stderr (python unittest, turbo, node). Continue keeps the output
        # visible while the $LASTEXITCODE check below still catches real failures.
        $ErrorActionPreference = 'Continue'
        & $Command @Arguments
        if ($LASTEXITCODE -ne 0) { throw "$Command exited with $LASTEXITCODE" }
    } catch {
        $script:failures.Add("$Label`: $($_.Exception.Message)")
    } finally {
        $ErrorActionPreference = $previousErrorAction
        Pop-Location
    }
}

function Assert-Structure {
    Write-Host "`n== Repository structure ==" -ForegroundColor Cyan
    $yaml = @(Get-ChildItem (Join-Path $repoRoot 'apps\server\internal\app') -File -Recurse |
        Where-Object Extension -in '.yaml', '.yml')
    foreach ($file in $yaml) { $script:failures.Add("YAML is forbidden under apps/server/internal/app: $($file.FullName)") }

    $tests = @(Get-ChildItem $repoRoot -Recurse -File -Filter '*_test.go' |
        Where-Object { $_.FullName -notmatch '[\\/](\.local|vendor)[\\/]' })
    foreach ($test in $tests) {
        $productionName = $test.Name.Substring(0, $test.Name.Length - '_test.go'.Length) + '.go'
        $productionPath = Join-Path $test.DirectoryName $productionName
        if (-not (Test-Path -LiteralPath $productionPath)) {
            $script:failures.Add("Go test requires exact sibling production file: $($test.FullName) -> $productionName")
        }
    }

    $testFiles = @(Get-ChildItem $repoRoot -Recurse -File |
        Where-Object {
            $_.FullName -notmatch '[\\/](node_modules|dist|\.local|vendor)[\\/]' -and
            ($_.Name -like '*_test.go' -or $_.Name -like '*.test.ts' -or $_.Name -like '*.test.tsx')
        })
    $forbidden = '(?i)\b(mock|fake|stub|miniredis|monkey\s*patch)\b'
    foreach ($file in $testFiles) {
        $matches = @(Select-String -LiteralPath $file.FullName -Pattern $forbidden)
        foreach ($match in $matches) {
            $script:failures.Add("Forbidden test substitute in $($file.FullName):$($match.LineNumber)")
        }
    }


    $externalIamFiles = @((Join-Path $repoRoot 'apps\server\go.mod'), (Join-Path $repoRoot 'apps\server\go.sum')) +
        @(Get-ChildItem (Join-Path $repoRoot 'apps\server\deploy') -Recurse -File |
            Where-Object { $_.Name -notlike '*.env' -and $_.FullName -notmatch '[\\/](secrets|\.local)[\\/]' }) +
        @(Get-ChildItem (Join-Path $repoRoot 'apps\server\cmd'), (Join-Path $repoRoot 'apps\server\internal'), (Join-Path $repoRoot 'apps\server\pkg') -Recurse -File -Filter '*.go' |
            Where-Object Name -notlike '*_test.go')
    $forbiddenIam = '(?i)\b(zitadel|spicedb|authzed|ory)\b'
    foreach ($file in $externalIamFiles) {
        $target = if ($file -is [System.IO.FileInfo]) { $file.FullName } else { [string]$file }
        $hits = @(Select-String -LiteralPath $target -Pattern $forbiddenIam)
        foreach ($hit in $hits) {
            $script:failures.Add("Forbidden external IAM/PDP dependency in ${target}:$($hit.LineNumber)")
        }
    }
    $supportedLocales = @('en-US', 'zh-CN', 'ms-MY')
    $baseCatalog = Get-Content -Raw -Encoding UTF8 -LiteralPath (Join-Path $repoRoot 'apps\server\pkg\i18n\locales\en-US.json') | ConvertFrom-Json
    $baseErrorKeys = [Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
    foreach ($key in $baseCatalog.PSObject.Properties.Name) { $null = $baseErrorKeys.Add($key) }
    $moduleErrorKeys = @{}
    $modules = @(Get-ChildItem (Join-Path $repoRoot 'apps\server\internal\modules') -Directory)
    foreach ($module in $modules) {
        $registration = Join-Path $module.FullName 'i18n.go'
        if (-not (Test-Path -LiteralPath $registration)) {
            $script:failures.Add("Module i18n registration is missing: $($module.FullName)\i18n.go")
        }

        $messagesByLocale = @{}
        foreach ($locale in $supportedLocales) {
            $localeFile = Join-Path $module.FullName "i18n\locales\$locale.json"
            if (-not (Test-Path -LiteralPath $localeFile)) {
                $script:failures.Add("Module locale is missing: $localeFile")
                continue
            }
            try {
                $messages = Get-Content -Raw -Encoding UTF8 -LiteralPath $localeFile | ConvertFrom-Json
                $messagesByLocale[$locale] = $messages
                foreach ($message in $messages.PSObject.Properties) {
                    if ([string]::IsNullOrWhiteSpace([string]$message.Value)) {
                        $script:failures.Add("Module locale has an empty translation: $localeFile -> $($message.Name)")
                    }
                }
            } catch {
                $script:failures.Add("Module locale is invalid JSON: $localeFile`: $($_.Exception.Message)")
            }
        }

        if (-not $messagesByLocale.ContainsKey('en-US')) { continue }
        $base = $messagesByLocale['en-US']
        $baseKeys = @($base.PSObject.Properties.Name | Sort-Object)
        $moduleErrorKeys[$module.Name] = [Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
        foreach ($key in $baseKeys) { $null = $moduleErrorKeys[$module.Name].Add($key) }
        foreach ($locale in @('zh-CN', 'ms-MY')) {
            if (-not $messagesByLocale.ContainsKey($locale)) { continue }
            $translated = $messagesByLocale[$locale]
            $translatedKeys = @($translated.PSObject.Properties.Name | Sort-Object)
            foreach ($key in $baseKeys) {
                if ($key -notin $translatedKeys) {
                    $script:failures.Add("Module locale is missing key: $($module.Name)/$locale -> $key")
                    continue
                }
                $baseValue = [string]$base.PSObject.Properties[$key].Value
                $translatedValue = [string]$translated.PSObject.Properties[$key].Value
                $formatPattern = '%(?:\[[0-9]+\])?[+#0\- ]*(?:[0-9]+|\*)?(?:\.(?:[0-9]+|\*))?[a-zA-Z%]'
                $baseFormats = @([regex]::Matches($baseValue, $formatPattern) | ForEach-Object Value | Sort-Object)
                $translatedFormats = @([regex]::Matches($translatedValue, $formatPattern) | ForEach-Object Value | Sort-Object)
                if (($baseFormats -join ',') -ne ($translatedFormats -join ',')) {
                    $script:failures.Add("Module locale format placeholders differ: $($module.Name)/$locale -> $key")
                }
            }
            foreach ($key in $translatedKeys) {
                if ($key -notin $baseKeys) {
                    $script:failures.Add("Module locale has an extra key: $($module.Name)/$locale -> $key")
                }
            }
        }
    }

    $directErrorPattern = '(?:huma\.(?:NewError|Error[A-Za-z0-9]+)|respx\.(?:Err|WriteError))\([^\r\n]*?"([a-z][a-z0-9_]*)"'
    $oauthErrorPattern = 'oauthError\([^\r\n]*?"([a-z][a-z0-9_]*)"'
    $productionFiles = @(Get-ChildItem (Join-Path $repoRoot 'apps\server\internal'), (Join-Path $repoRoot 'apps\server\pkg') -Recurse -File -Filter '*.go' |
        Where-Object Name -notlike '*_test.go')
    foreach ($file in $productionFiles) {
        $owner = $null
        if ($file.FullName -match '[\\/]internal[\\/]modules[\\/]([^\\/]+)[\\/]') { $owner = $Matches[1] }
        $content = Get-Content -Raw -Encoding UTF8 -LiteralPath $file.FullName
        foreach ($match in [regex]::Matches($content, $directErrorPattern)) {
            $key = $match.Groups[1].Value
            $owned = $owner -and $moduleErrorKeys.ContainsKey($owner) -and $moduleErrorKeys[$owner].Contains($key)
            if (-not $baseErrorKeys.Contains($key) -and -not $owned) {
                $script:failures.Add("Public error key has no owning locale: $($file.FullName) -> $key")
            }
        }
        foreach ($match in [regex]::Matches($content, $oauthErrorPattern)) {
            $key = 'oauth_' + $match.Groups[1].Value
            if (-not $moduleErrorKeys['oauth'].Contains($key)) {
                $script:failures.Add("OAuth protocol error key has no owning locale: $($file.FullName) -> $key")
            }
        }
    }
}

function Resolve-Scopes {
    if ($Scope -eq 'all') { return @{ backend = $true; frontend = $true; docs = $true } }
    if ($Scope -ne 'auto') {
        return @{
            backend = $Scope -eq 'backend'
            frontend = $Scope -eq 'frontend'
            docs = $Scope -eq 'docs'
        }
    }

    $paths = @($ChangedPath)
    if ($paths.Count -eq 0) {
        Push-Location $repoRoot
        try {
            $paths += @(& git diff --name-only HEAD)
            $paths += @(& git ls-files --others --exclude-standard)
        } finally { Pop-Location }
    }
    $paths = @($paths | Where-Object { $_ } | Sort-Object -Unique)
    if ($paths.Count -eq 0) { return @{ backend = $true; frontend = $true; docs = $true } }
    return @{
        backend = [bool]($paths | Where-Object { $_ -match '^(cmd|internal|pkg|deploy)/|^go\.(mod|sum)$|^Dockerfile$|^\.claude/skills/dev-backend/' })
        frontend = [bool]($paths | Where-Object { $_ -match '^apps/admin/|^\.claude/skills/dev-frontend/' })
        docs = [bool]($paths | Where-Object { $_ -match '^apps/docs/|^README\.md$|^\.claude/skills/dev-docs/' })
    }
}

Invoke-NativeStep 'Refresh repository facts' $repoRoot 'python' @($skillRuntime, 'refresh')
Assert-Structure
$selected = Resolve-Scopes

Invoke-NativeStep 'Validate repository skills' $repoRoot 'python' @($skillRuntime, 'validate')
Invoke-NativeStep 'Test repository skill runtime' $repoRoot 'python' @('-m', 'unittest', 'discover', '-s', '.claude/skills/dev-quality-gate/scripts', '-p', 'test_*.py')

if ($selected.backend) {
    $env:GOSUMDB = 'sum.golang.org'
    # Monorepo: backend Go modules live under apps/ (no root go.mod).
    $goModules = @('apps\server', 'apps\server-ai')
    $unformatted = @()
    Get-ChildItem $repoRoot -Recurse -File -Filter '*.go' |
        Where-Object { $_.FullName -notmatch '[\\/](\.local|vendor)[\\/]' } |
        ForEach-Object { $u = & gofmt -l $_.FullName; if ($LASTEXITCODE -ne 0) { $script:failures.Add("gofmt failed: $($_.FullName)") }; if ($u) { $script:unformatted += $u } }
    foreach ($file in $unformatted) { $failures.Add("Go file is not formatted: $file") }
    foreach ($goMod in $goModules) {
        $modRoot = Join-Path $repoRoot $goMod
        Invoke-NativeStep "Go vet ($goMod)" $modRoot 'go' @('vet', './...')
    }
    $golangciLint = Get-Command golangci-lint -ErrorAction SilentlyContinue
    $lintCommand = if ($golangciLint) { $golangciLint.Source } else { '' }
    if (-not $lintCommand) {
        $goPath = (& go env GOPATH).Trim()
        $candidate = Join-Path $goPath 'bin\golangci-lint.exe'
        if (Test-Path $candidate) { $lintCommand = $candidate }
    }
    if ($lintCommand) {
        foreach ($goMod in $goModules) {
            Invoke-NativeStep "Go static analysis (golangci-lint) ($goMod)" (Join-Path $repoRoot $goMod) $lintCommand @('run', './...')
        }
    }
    else { $failures.Add('golangci-lint is required for the backend gate.') }
    if ($Full) {
        foreach ($goMod in $goModules) {
            $modRoot = Join-Path $repoRoot $goMod
            Invoke-NativeStep "Go race tests ($goMod)" $modRoot 'go' @('test', '-race', '-covermode=atomic', "./...")
        }
        $govulncheck = Get-Command govulncheck -ErrorAction SilentlyContinue
        $scannerCommand = if ($govulncheck) { $govulncheck.Source } else { '' }
        if (-not $scannerCommand) {
            $goPath = (& go env GOPATH).Trim()
            $candidate = Join-Path $goPath 'bin\govulncheck.exe'
            if (Test-Path $candidate) { $scannerCommand = $candidate }
        }
        if ($scannerCommand) {
            foreach ($goMod in $goModules) {
                Invoke-NativeStep "Go vulnerability scan ($goMod)" (Join-Path $repoRoot $goMod) $scannerCommand @('./...')
            }
        }
        else { $failures.Add('govulncheck is required for the full backend gate.') }
    } else {
        foreach ($goMod in $goModules) {
            Invoke-NativeStep "Go tests ($goMod)" (Join-Path $repoRoot $goMod) 'go' @('test', './...')
        }
    }
}

if ($selected.frontend) {
    $adminRoot = Join-Path $repoRoot 'apps\admin'
    foreach ($file in @('Dockerfile', 'nginx.conf')) {
        if (-not (Test-Path (Join-Path $adminRoot $file))) { $failures.Add("Frontend deployment file is missing: apps/admin/$file") }
    }
    Invoke-NativeStep 'Frontend lint' $adminRoot 'bun' @('run', 'lint')
    Invoke-NativeStep 'Frontend typecheck' $adminRoot 'bun' @('run', 'typecheck')
    Invoke-NativeStep 'Frontend tests' $adminRoot 'bun' @('run', 'test')
    Invoke-NativeStep 'Frontend production build' $adminRoot 'bun' @('run', 'build')
}

if ($selected.docs) {
    $docsRoot = Join-Path $repoRoot 'apps\docs'
    Invoke-NativeStep 'Documentation install' $docsRoot 'bun' @('install', '--frozen-lockfile')
    Invoke-NativeStep 'Documentation production build' $docsRoot 'bun' @('run', 'build')
}

if ($failures.Count -gt 0) {
    Write-Host "`nQuality gate failed ($($failures.Count)):" -ForegroundColor Red
    $failures | ForEach-Object { Write-Host "- $_" -ForegroundColor Red }
    exit 1
}

Write-Host "`nQuality gate passed." -ForegroundColor Green
