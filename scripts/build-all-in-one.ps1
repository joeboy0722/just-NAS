param([switch]$SkipTests,[string]$DockerHost,[string]$OutputDirectory="output",[string]$CacheVolume="just-nas-iso-cache")
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
$names = 'CGO_ENABLED','GOOS','GOARCH','GOCACHE','GIT_CONFIG_COUNT','GIT_CONFIG_KEY_0','GIT_CONFIG_VALUE_0','DOCKER_BUILDKIT'
$previous = @{}
foreach ($name in $names) { $previous[$name] = [Environment]::GetEnvironmentVariable($name,'Process') }
function Invoke-Native { param([string]$File,[Parameter(ValueFromRemainingArguments=$true)][string[]]$Arguments)
    if ($File -eq 'docker' -and $DockerHost) { $Arguments = @('--host',$DockerHost) + $Arguments }
    & $File @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$File failed with exit code $LASTEXITCODE" }
}
try {
    New-Item -ItemType Directory -Path output -Force | Out-Null
    [IO.File]::WriteAllText((Join-Path $root 'output/go.mod'), "module just-nas.local/build-artifacts`n")
    $licenses = Join-Path $root 'output/compliance/iso'
    if ($env:NAS_SOURCE_BOOTSTRAP -ne '1' -and -not (Test-Path -LiteralPath (Join-Path $licenses 'READY'))) {
        throw 'Missing third-party license collection. Extract the matching Release licenses archive into output/compliance first; see COMPLIANCE.md.'
    }
    $env:GOCACHE = Join-Path $root 'output/go-cache'
    $env:GIT_CONFIG_COUNT='1'; $env:GIT_CONFIG_KEY_0='safe.directory'; $env:GIT_CONFIG_VALUE_0=$root.Replace('\','/')
    if (-not $SkipTests) {
        Invoke-Native -File go -Arguments @('test','./...')
        Invoke-Native -File go -Arguments @('vet','./...')
    }
    $env:CGO_ENABLED='0'; $env:GOOS='linux'; $env:GOARCH='amd64'
    Invoke-Native -File go -Arguments @('build','-trimpath','-ldflags=-s -w','-o','nas-server-linux','./cmd/nas-server')
    Invoke-Native -File docker -Arguments @('info','--format','{{.ServerVersion}}')
    $env:DOCKER_BUILDKIT='0'
    Invoke-Native -File docker -Arguments @('build','--memory=3g','--cpu-period=100000','--cpu-quota=200000','-t','just-nas-builder','-f','Dockerfile.iso-builder','.')
    $outDir = [IO.Path]::GetFullPath((Join-Path $root $OutputDirectory))
    if (-not $outDir.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Output directory must stay inside the workspace' }
    New-Item -ItemType Directory -Path $outDir -Force | Out-Null
    $baseline = Join-Path $outDir 'alpine-nas-full.iso'
    if (-not (Test-Path -LiteralPath $baseline)) { Copy-Item -LiteralPath (Join-Path $root 'output/alpine-nas-full.iso') -Destination $baseline }
    $server = Join-Path $root 'nas-server-linux'
    # Use the LF-normalized scripts already copied into the builder image.
    Invoke-Native -File docker -Arguments @('run','--rm','--cpus=2','--memory=3g','--pids-limit=512','-e','NAS_SOURCE_BOOTSTRAP','-v',"${CacheVolume}:/iso-cache",'-v',"${outDir}:/builder/output",'-v',"${licenses}:/builder/compliance:ro",'-v',"${server}:/builder/nas-server-linux:ro",'just-nas-builder','sh','/builder/scripts/make-custom-iso.sh')
    Write-Host "ISO candidate: $outDir/alpine-nas-stores.iso"
} finally {
    foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name,$previous[$name],'Process') }
    Pop-Location
}

