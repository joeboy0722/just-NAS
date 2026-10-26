param([switch]$Execute,[switch]$AllowIncompleteTCG,[string]$HostAddress="192.168.56.101",[string]$Identity,[string]$KnownHosts,[string]$PoolGuid,[string]$TCGEvidence)
$ErrorActionPreference='Stop'
$argsList=@((Join-Path $PSScriptRoot 'build-on-nas.py'),'--host',$HostAddress)
if ($Execute) { $argsList+=@('--execute','--identity',$Identity,'--known-hosts',$KnownHosts,'--pool-guid',$PoolGuid,'--tcg-evidence',$TCGEvidence) }
if ($AllowIncompleteTCG) { $argsList+='--allow-incomplete-tcg' }
& python @argsList
if ($LASTEXITCODE -ne 0) { throw 'NAS build failed; no Windows Docker fallback was attempted' }
