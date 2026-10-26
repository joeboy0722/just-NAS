"""Explicit administrator-only, persistent just NAS mode boot entry. Preserves the default OS."""
import argparse,ctypes,datetime,json,os,pathlib,re,subprocess,time
R=pathlib.Path(__file__).resolve().parents[1];OUT=R/'output/kvm-host-test';OUT.mkdir(parents=True,exist_ok=True)
STATUS=OUT/'boot-preparation.json';BCD=str(pathlib.Path(os.environ.get('SystemRoot','C:/Windows'))/'System32/bcdedit.exe')
def status(phase,**values):
 data={'phase':phase,**values};tmp=STATUS.with_suffix('.new');tmp.write_text(json.dumps(data,indent=2),encoding='utf-8');tmp.replace(STATUS)
def run(args):
 p=subprocess.run(args,capture_output=True);raw=p.stdout+p.stderr
 encoding='utf-8' if pathlib.Path(args[0]).name.lower()=='powershell.exe' else ('utf-16-le' if b'\x00' in raw[:100] else 'cp'+str(ctypes.windll.kernel32.GetOEMCP()))
 text=raw.decode(encoding,errors='replace')
 with (OUT/'admin-operations.log').open('a',encoding='utf-8') as f:f.write(json.dumps(args)+'\n'+text+'\n')
 if p.returncode:raise RuntimeError('Command failed '+json.dumps(args)+': '+text[:1500])
 return text

def powershell(code):
 return run(['powershell.exe','-NoProfile','-Command',"[Console]::OutputEncoding=[Text.UTF8Encoding]::new(); $ErrorActionPreference='Stop'; "+code])

def boot_default():
 text=run([BCD,'/enum','{bootmgr}','/v'])
 match=re.search(r'(?im)^\s*(?:default|預設|默认)\s+(\{[a-f0-9-]{36}\})',text)
 if not match:raise RuntimeError('Cannot verify original default from BCDEdit output; no boot changes made')
 timeout=re.search(r'(?im)^\s*timeout\s+(\d+)',text)
 sequence=bool(re.search(r'(?im)^\s*bootsequence\s+',text))
 return json.dumps({'default':match.group(1).lower(),'bootsequence':[True] if sequence else [],'timeout':int(timeout.group(1)) if timeout else None})

def inventory():
 return json.loads(powershell(r"$b=Get-BitLockerVolume -MountPoint $env:SystemDrive; $v=@(); if(Get-Command Get-VM -ErrorAction SilentlyContinue){$v=@(Get-VM | Where-Object {$_.State -ne 'Off'} | Select-Object Name,@{n='State';e={$_.State.ToString()}})}; $lsa=Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\Lsa' -ErrorAction SilentlyContinue; $policy=Get-ItemProperty 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\DeviceGuard' -ErrorAction SilentlyContinue; $hvci=Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\DeviceGuard\Scenarios\HypervisorEnforcedCodeIntegrity' -ErrorAction SilentlyContinue; @{bitlocker_protection=$b.ProtectionStatus.ToString();bitlocker_volume=$b.VolumeStatus.ToString();lsa_lock=$lsa.LsaCfgFlags;policy_lock=$policy.LsaCfgFlags;hvci_lock=$hvci.Locked;hyperv_guests=$v;features=@(Get-WindowsOptionalFeature -Online | Where-Object {$_.FeatureName -match 'Hyper-V|HypervisorPlatform|VirtualMachinePlatform|Subsystem-Linux|DisposableClientVM'} | Select-Object FeatureName,@{n='State';e={$_.State.ToString()}})} | ConvertTo-Json -Depth 6"))

def wait_for(name,seconds=600):
 for _ in range(seconds):
  if (OUT/name).is_file():return
  time.sleep(1)
 raise RuntimeError('Timed out waiting for '+name)

def main():
 a=argparse.ArgumentParser(description=__doc__);a.add_argument('--execute',action='store_true');v=a.parse_args()
 if not v.execute:
  print('PREVIEW: require admin; check BitLocker/UEFI locks/other VMs; back up BCD; copy current entry as just NAS mode; set only the copy hypervisorlaunchtype/vsmlaunchtype off; preserve default; append to menu with 15-second timeout. Wait for NAS shutdown and explicit reboot signal.');return
 if os.name!='nt' or not ctypes.windll.shell32.IsUserAnAdmin():raise RuntimeError('Windows administrator elevation required')
 entry=None;before=None
 try:
  inv=inventory();(OUT/'admin-inventory.json').write_text(json.dumps(inv,indent=2),encoding='utf-8')
  if inv['bitlocker_protection']!='Off':raise RuntimeError('BitLocker protection is active; recovery readiness must be resolved before changing boot settings')
  if inv.get('lsa_lock')==1 or inv.get('policy_lock')==1 or inv.get('hvci_lock')==1:raise RuntimeError('UEFI-locked security policy requires separate review; no global policy or Secure Boot changes made')
  if inv.get('hyperv_guests'):raise RuntimeError('Other Hyper-V guests are active; preserve them rather than stopping them forcibly')
  before=json.loads(boot_default())
  if any(before.get('bootsequence',[])):raise RuntimeError('Another one-time boot sequence is already pending')
  (OUT/'boot-manager-before.txt').write_text(run([BCD,'/enum','{bootmgr}','/v']),encoding='utf-8')
  (OUT/'current-entry-before.txt').write_text(run([BCD,'/enum','{current}','/v']),encoding='utf-8')
  backup=OUT/('bcd-backup-'+datetime.datetime.now().strftime('%Y%m%d-%H%M%S'))
  run([BCD,'/export',str(backup)])
  copied=run([BCD,'/copy','{current}','/d','just NAS mode'])
  match=re.search(r'\{[a-fA-F0-9-]{36}\}',copied)
  if not match:raise RuntimeError('Cannot identify newly copied BCD entry')
  entry=match.group(0)
  run([BCD,'/set',entry,'hypervisorlaunchtype','off']);run([BCD,'/set',entry,'vsmlaunchtype','off'])
  text=run([BCD,'/enum',entry,'/v']);(OUT/'test-entry.txt').write_text(text,encoding='utf-8')
  if not re.search(r'hypervisorlaunchtype\s+Off',text,re.I) or not re.search(r'vsmlaunchtype\s+Off',text,re.I):raise RuntimeError('Test entry options not verified')
  run([BCD,'/displayorder',entry,'/addlast']);run([BCD,'/timeout','15'])
  after=json.loads(boot_default())
  if after['default']!=before['default'] or after['bootsequence']!=before['bootsequence']:raise RuntimeError('Original default or boot sequence changed')
  (OUT/'boot-manager-after.txt').write_text(run([BCD,'/enum','{bootmgr}','/v']),encoding='utf-8')
  details={'entry':entry,'original_default':before['default'],'backup':str(backup),'persistent':True,'name':'just NAS mode','menu_timeout_seconds':15}
  status('PREPARED_WAITING_FOR_NAS_SHUTDOWN',**details)
  wait_for('nas-powered-off.signal')
  powershell("foreach($n in @('vmcompute','vmms','WslService')){$s=Get-Service $n -ErrorAction SilentlyContinue; if($s -and $s.Status -eq 'Running'){try{Stop-Service $n -ErrorAction Stop}catch{Write-Warning $_}}}; Get-Service vmcompute,vmms,WslService -ErrorAction SilentlyContinue | Select-Object Name,@{n='Status';e={$_.Status.ToString()}} | ConvertTo-Json -Compress")
  status('READY_TO_REBOOT',**details)
  wait_for('reboot.signal')
  status('REBOOT_REQUESTED',delay_seconds=30,**details)
  time.sleep(30)
  # No /f, and /t 0 does not imply forced app termination.
  run(['shutdown.exe','/r','/t','0'])
 except Exception as error:
  cleanup=[]
  if entry:
   rollback=[[BCD,'/delete',entry,'/cleanup']]
   if before is not None:
    rollback.append([BCD,'/timeout',str(before['timeout'])] if before['timeout'] is not None else [BCD,'/deletevalue','{bootmgr}','timeout'])
   for args in rollback:
    try:run(args)
    except Exception as e:cleanup.append(str(e))
  status('BLOCKED',reason=str(error),entry=entry,cleanup_errors=cleanup)
  raise
if __name__=='__main__':main()
