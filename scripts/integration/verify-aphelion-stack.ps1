[CmdletBinding()]
param(
	[Parameter(Mandatory = $true)]
	[string] $AphelionRoot,
	[string] $MeridianMcpRoot,
	[string] $MeridianRiftRoot,
	[string] $InstalledMcp,
	[string] $EvidencePath,
	[int] $TimeoutMinutes = 30,
	[switch] $PlanOnly
)

$ErrorActionPreference = "Stop"
$script:GateResults = [System.Collections.Generic.List[object]]::new()
$script:RunStarted = [DateTimeOffset]::UtcNow
$script:RunID = $script:RunStarted.ToString("yyyyMMddTHHmmssZ") + "-" + [guid]::NewGuid().ToString("N")

function Resolve-RequiredRoot {
	param([string] $Path, [string] $Name)
	if ([string]::IsNullOrWhiteSpace($Path)) { throw "$Name is required unless -PlanOnly is used." }
	if (-not (Test-Path -LiteralPath $Path -PathType Container)) { throw "$Name does not exist: $Path" }
	return (Resolve-Path -LiteralPath $Path).Path
}

function ConvertTo-ProcessArgument {
	param([string] $Value)
	if ($Value -notmatch '[\s"]') { return $Value }
	return '"' + ($Value -replace '(\\*)"', '$1$1\"' -replace '(\\+)$', '$1$1') + '"'
}

function Stop-ProcessTree {
	param([System.Diagnostics.Process] $Process)
	if (-not $Process -or $Process.HasExited) { return }
	& taskkill.exe /PID $Process.Id /T /F 2>$null | Out-Null
	try { $Process.Kill() } catch { }
}

function Add-GateResult {
	param(
		[string] $Name,
		[string] $Repository,
		[string] $Status,
		[int] $ExitCode,
		[double] $DurationSeconds,
		[string] $Log,
	[string] $Detail = ""
	)
	$stderrLog = ""
	if ($Log -like "*.stdout.log") { $stderrLog = $Log.Substring(0, $Log.Length - ".stdout.log".Length) + ".stderr.log" }
	[void]$script:GateResults.Add([ordered]@{
		name = $Name
		repository = $Repository
		status = $Status
		exit_code = $ExitCode
		duration_seconds = [Math]::Round($DurationSeconds, 3)
		stdout_log = $Log
		stderr_log = $stderrLog
		detail = $Detail
	})
}

function Invoke-Gate {
	param(
		[string] $Name,
		[string] $Repository,
		[string] $Executable,
		[string[]] $Arguments,
		[string] $WorkingDirectory,
		[hashtable] $Environment = @{},
		[int] $TimeoutSeconds = ($TimeoutMinutes * 60)
	)
	$gateRoot = Join-Path $script:EvidenceRoot "logs"
	New-Item -ItemType Directory -Force -Path $gateRoot | Out-Null
	$slug = ($Name -replace '[^A-Za-z0-9._-]', '-').ToLowerInvariant()
	$stdout = Join-Path $gateRoot "$slug.stdout.log"
	$stderr = Join-Path $gateRoot "$slug.stderr.log"
	$started = [DateTimeOffset]::UtcNow
	$previous = @{}
	try {
		foreach ($key in $Environment.Keys) {
			$previous[$key] = [Environment]::GetEnvironmentVariable($key, "Process")
			[Environment]::SetEnvironmentVariable($key, [string]$Environment[$key], "Process")
		}
		$argumentLine = ($Arguments | ForEach-Object { ConvertTo-ProcessArgument ([string]$_) }) -join ' '
		$process = Start-Process -FilePath $Executable -ArgumentList $argumentLine -WorkingDirectory $WorkingDirectory -WindowStyle Hidden -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
		if (-not $process.WaitForExit($TimeoutSeconds * 1000)) {
			Stop-ProcessTree $process
			Add-GateResult $Name $Repository "timeout" -1 (([DateTimeOffset]::UtcNow - $started).TotalSeconds) $stdout "Exceeded ${TimeoutSeconds}s; process tree terminated."
			return $false
		}
		$process.WaitForExit()
		$combinedBytes = 0
		foreach ($path in @($stdout, $stderr)) {
			if (Test-Path -LiteralPath $path) { $combinedBytes += (Get-Item -LiteralPath $path).Length }
		}
		if ($combinedBytes -gt 16MB) {
			Add-GateResult $Name $Repository "failed" -1 (([DateTimeOffset]::UtcNow - $started).TotalSeconds) $stdout "Combined output exceeded 16 MiB."
			return $false
		}
		$status = if ($process.ExitCode -eq 0) { "passed" } else { "failed" }
		Add-GateResult $Name $Repository $status $process.ExitCode (([DateTimeOffset]::UtcNow - $started).TotalSeconds) $stdout
		return $process.ExitCode -eq 0
	}
	catch {
		Add-GateResult $Name $Repository "failed" -1 (([DateTimeOffset]::UtcNow - $started).TotalSeconds) $stdout $_.Exception.Message
		return $false
	}
	finally {
		foreach ($key in $Environment.Keys) {
			[Environment]::SetEnvironmentVariable($key, $previous[$key], "Process")
		}
	}
}

function Add-UnavailableGate {
	param([string] $Name, [string] $Repository, [string] $Reason)
	Add-GateResult $Name $Repository "unavailable" -1 0 "" $Reason
}

function Write-Evidence {
	$passed = @($script:GateResults | Where-Object { $_.status -eq "passed" }).Count
	$complete = $script:GateResults.Count -gt 0 -and $passed -eq $script:GateResults.Count
	$evidence = [ordered]@{
		schema_version = 2
		started_at = $script:RunStarted.ToString("o")
		completed_at = [DateTimeOffset]::UtcNow.ToString("o")
		integration_verified = $complete
		gates = $script:GateResults
	}
	$parent = Split-Path -Parent $EvidencePath
	New-Item -ItemType Directory -Force -Path $parent | Out-Null
	$temporary = "$EvidencePath.tmp"
	$evidence | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $temporary -Encoding UTF8
	Move-Item -LiteralPath $temporary -Destination $EvidencePath -Force
	Write-Host "Aphelion stack evidence: $EvidencePath"
	return $complete
}

$AphelionRoot = Resolve-RequiredRoot $AphelionRoot "AphelionRoot"
if ([string]::IsNullOrWhiteSpace($EvidencePath)) {
	$EvidencePath = Join-Path $AphelionRoot ".artifacts\stack\evidence.json"
}
$EvidencePath = [System.IO.Path]::GetFullPath($EvidencePath)
$script:EvidenceRoot = Split-Path -Parent $EvidencePath

if ($PlanOnly) {
	Add-GateResult "Aphelion integration contracts" "AphelionDMM" "planned" 0 0 "" "Available in this checkout."
	foreach ($candidate in @(
		@("Installed Meridian-MCP conformance", "Meridian-MCP", $InstalledMcp),
		@("Staged Meridian inspection", "map repository", $MeridianRiftRoot)
	)) {
		if ([string]::IsNullOrWhiteSpace($candidate[2]) -or -not (Test-Path -LiteralPath $candidate[2])) {
			Add-UnavailableGate $candidate[0] $candidate[1] "Dependency is intentionally unavailable in the single-repository CI checkout."
		}
		else {
			Add-GateResult $candidate[0] $candidate[1] "planned" 0 0 "" "Dependency path is available."
		}
	}
	Write-Evidence | Out-Null
	exit 0
}

$MeridianMcpRoot = Resolve-RequiredRoot $MeridianMcpRoot "MeridianMcpRoot"
$MeridianRiftRoot = Resolve-RequiredRoot $MeridianRiftRoot "MeridianRiftRoot"
if (-not (Test-Path -LiteralPath $InstalledMcp -PathType Leaf)) { throw "InstalledMcp does not exist: $InstalledMcp" }
$InstalledMcp = (Resolve-Path -LiteralPath $InstalledMcp).Path

$toolchainText = Get-Content -LiteralPath (Join-Path $MeridianMcpRoot "rust-toolchain.toml") -Raw
if ($toolchainText -notmatch 'channel\s*=\s*"([^"]+)"') { throw "Meridian-MCP rust-toolchain.toml has no channel." }
$meridianToolchain = $Matches[1]
$goBin = (& go.exe env GOBIN).Trim()
if ([string]::IsNullOrWhiteSpace($goBin)) {
	$goPath = (& go.exe env GOPATH).Trim().Split([System.IO.Path]::PathSeparator)[0]
	$goBin = Join-Path $goPath "bin"
}
$goToolEnvironment = @{ PATH = $goBin + [System.IO.Path]::PathSeparator + $env:PATH }

[void](Invoke-Gate "Aphelion Go contracts" "AphelionDMM" "go.exe" @("test", "./...", "-count=1") $AphelionRoot)
$rustTarget = "1.82-x86_64-pc-windows-gnu"
$goToolEnvironment.RUST_TARGET = $rustTarget
[void](Invoke-Gate "Aphelion Windows resources" "AphelionDMM" "task.exe" @("task_win:gen_syso") $AphelionRoot $goToolEnvironment)
[void](Invoke-Gate "Aphelion cross-stack build" "AphelionDMM" "task.exe" @("build") $AphelionRoot $goToolEnvironment)

[void](Invoke-Gate "Meridian-MCP pinned tests" "Meridian-MCP" "rustup.exe" @("run", $meridianToolchain, "cargo", "test", "--locked", "--all-targets") $MeridianMcpRoot)

$stageRoot = Join-Path $script:EvidenceRoot ("stages\" + $script:RunID)
$targetID = "virtual-domains/test-only"
$targetRelative = "_maps/virtual_domains/test_only.dmm"
$sourceTarget = Join-Path $MeridianRiftRoot ($targetRelative.Replace('/', [System.IO.Path]::DirectorySeparatorChar))
$repositoryRevision = (& git.exe -C $MeridianRiftRoot rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($repositoryRevision)) { throw "Could not read Meridian-Rift revision." }
$dmePath = Join-Path $MeridianRiftRoot "tgstation.dme"
$manifestPath = Join-Path $script:EvidenceRoot "meridian-stage-manifest.json"
$manifest = [ordered]@{
	schema_version = 1
	repository_identity = "meridian-rift"
	repository_revision = $repositoryRevision
	dme_identifier = "tgstation.dme"
	map_target_id = $targetID
	protocol_version = 1
	environment_sha256 = (Get-FileHash -LiteralPath $dmePath -Algorithm SHA256).Hash.ToLowerInvariant()
	input_map_sha256 = (Get-FileHash -LiteralPath $sourceTarget -Algorithm SHA256).Hash.ToLowerInvariant()
	output_map_sha256 = (Get-FileHash -LiteralPath $sourceTarget -Algorithm SHA256).Hash.ToLowerInvariant()
	accepted_revision = 1
	producer = [ordered]@{ name = "AphelionDMM"; version = "stack-verifier" }
}
$manifestJSON = $manifest | ConvertTo-Json -Depth 4
[System.IO.File]::WriteAllText($manifestPath, $manifestJSON, [System.Text.UTF8Encoding]::new($false))

$stagePassed = Invoke-Gate "Shipped Meridian stage inspection" "AphelionDMM + Meridian-MCP" "go.exe" @(
	"run", "./cmd/apheliondmm-meridian-verify",
	"--repository-root", $MeridianRiftRoot,
	"--repository-identity", "meridian-rift",
	"--dme", "tgstation.dme",
	"--map-target-id", $targetID,
	"--map-target", $targetRelative,
	"--stage-root", $stageRoot,
	"--manifest", $manifestPath,
	"--candidate", $sourceTarget,
	"--mcp-executable", $InstalledMcp,
	"--allow-dirty"
) $AphelionRoot @{} ($TimeoutMinutes * 60)
$stageLog = Join-Path $script:EvidenceRoot "logs\shipped-meridian-stage-inspection.stdout.log"
if ($stagePassed) {
	$stage = Get-Content -LiteralPath $stageLog -Raw | ConvertFrom-Json
	if ($stage.verifier_version -ne "2" -or $stage.exit_classification -ne "inspected") {
		throw "Shipped Meridian verifier did not return version 2 inspection evidence."
	}
}

$complete = Write-Evidence
if (-not $complete) { exit 1 }
