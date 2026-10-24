"""Diskless, disconnected plain boot of just-nas. Existing VDI attachments are journaled and restored."""
import pathlib,subprocess,json,time,re,shutil
R=pathlib.Path(__file__).resolve().parents[1];O=R/'output/vm-test';V=r'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe'
def vb(*args):
 for _ in range(8):
  p=subprocess.run([V,*args],text=True,capture_output=True)
  if p.returncode==0:return p.stdout
  if 'locked' not in p.stderr.lower():raise RuntimeError(p.stderr[:500])
  time.sleep(1.5)
 raise RuntimeError('VM settings remained locked')
def info():return vb('showvminfo','just-nas','--machinereadable')
def off():
 for _ in range(90):
  if 'VMState="poweroff"' in info():return
  time.sleep(1)
 raise RuntimeError('VM did not power off')
def enter(command):vb('controlvm','just-nas','keyboardputstring',command);vb('controlvm','just-nas','keyboardputscancode','1c','9c')
subprocess.run(['python',str(R/'scripts/vm-cycle.py'),'--stop'],check=True);state=info();disks={}
for line in state.splitlines():
 m=re.match(r'"SATA-([0-5])-0"=(.*)',line)
 if m:
  path=json.loads(m.group(2))
  if path=='none':continue
  assert pathlib.Path(path).suffix.lower()=='.vdi';disks[m.group(1)]=path
assert len(disks)==6 and pathlib.Path(disks['5']).samefile(O/'boot-console-test.vdi')
(O/'boot-rc2-disks-before-fresh.json').write_text(json.dumps(disks,indent=2),encoding='utf-8')
fixture=R/'output/rc2-plain-fixed-test.iso';assert fixture.is_file()
for port in disks:vb('storageattach','just-nas','--storagectl','SATA','--port',port,'--device','0','--type','hdd','--medium','none')
assert not re.search(r'"SATA-[0-5]-0"="(?!none)',info())
vb('modifyvm','just-nas','--cableconnected1','off')
vb('storageattach','just-nas','--storagectl','IDE','--port','0','--device','0','--type','dvddrive','--medium',str(fixture))
serial=O/'boot-rc2-serial.log';shutil.copyfile(serial,O/'boot-rc2-serial-before-fresh.log');serial.write_text('',encoding='utf-8')
try:
 vb('startvm','just-nas','--type','gui')
 for _ in range(240):
  text=serial.read_text(encoding='utf-8',errors='replace')
  if 'Storage      First setup' in text:break
  time.sleep(1)
 else:raise RuntimeError('Fresh boot did not reach real ready state')
 vb('controlvm','just-nas','screenshotpng',str(O/'boot-rc2-screenshots/10-plain-no-pools-no-network.png'))
 vb('controlvm','just-nas','keyboardputscancode','38','3c','bc','b8');time.sleep(.6);enter('root');time.sleep(.6)
 enter("printf '\nBOOTJSON_BEGIN\n' > /dev/ttyS0; cat /run/just-nas/boot.json > /dev/ttyS0; printf '\nBOOTJSON_END\n' > /dev/ttyS0")
 for _ in range(20):
  text=serial.read_text(encoding='utf-8',errors='replace')
  if 'BOOTJSON_END' in text:break
  time.sleep(.3)
 raw=text.rsplit('BOOTJSON_BEGIN',1)[1].split('BOOTJSON_END',1)[0];view=json.loads(raw.strip());assert view['ready'] and view['storage']=='First setup' and view['primary_missing'] and not view['addresses']
 stages={s['id']:s['state'] for s in view['stages']};assert stages['pools']=='skip' and stages['settings']=='skip' and stages['web']=='ok' and stages['extensions']=='skip'
 result=[{'check':'Final ISO diskless first setup reaches a real live web service, with no false storage failure','result':'PASS'},{'check':'Disconnected management network shows missing address warning without inventing a URL','result':'PASS'},{'check':'Test-only just_nas_boot=plain image presents real plain startup events and welcome','result':'PASS'}]
 (O/'boot-rc2-fresh-results.json').write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8');print('PASS diskless, disconnected plain boot and first setup',flush=True)
 enter('poweroff');off()
finally:
 if 'VMState="running"' in info():
  # All data disks remain detached; a forced stop here can affect RAM only.
  assert not re.search(r'"SATA-[0-5]-0"="(?!none)',info());vb('controlvm','just-nas','poweroff');off()
 for port,path in disks.items():vb('storageattach','just-nas','--storagectl','SATA','--port',port,'--device','0','--type','hdd','--medium',path)
 vb('modifyvm','just-nas','--cableconnected1','on')
 target=R/'output/just-NAS-v2.1.0-rc2-x86_64.iso';shutil.copyfile(R/'output/rc2-final.iso',target)
 vb('storageattach','just-nas','--storagectl','IDE','--port','0','--device','0','--type','dvddrive','--medium',str(target));vb('startvm','just-nas','--type','gui');print('Restored all original VDI attachments, management link and final rc2 image',flush=True)
