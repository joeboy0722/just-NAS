"""Create private unattended *test copies*. Never changes or publishes user media.

Generated credentials and derived ISO files live exclusively under ignored output/.
"""
import json,pathlib,secrets,subprocess,sys
R=pathlib.Path(__file__).resolve().parents[1];O=R/'output/vm-os-fixtures';O.mkdir(exist_ok=True)
C=R/'output/vm-test/test-guest-credentials.json'
if not C.exists():C.write_text(json.dumps({'user':'nas-test','password':secrets.token_urlsafe(21),'proof':secrets.token_hex(32)}),encoding='utf-8')
c=json.loads(C.read_text(encoding='utf-8'))
def write(n,s):
 p=O/n;p.parent.mkdir(parents=True,exist_ok=True);p.write_text(s,encoding='utf-8',newline='\n')
def docker(cmd,stdin=None):
 subprocess.run(['docker','run','--rm','-i','-v',str(O)+':/out','-v',r'D:/VM ISO:/media:ro','just-nas-builder-vm','sh','-ec',cmd],input=stdin,check=True)
if sys.argv[1]=='ubuntu':
 hashed=subprocess.check_output(['docker','run','--rm','-i','just-nas-builder-vm','openssl','passwd','-6','-stdin'],input=c['password'].encode()).decode().strip()
 proof='''import http.server,json,pathlib,hashlib,subprocess
p=pathlib.Path('/home/nas-test/just-nas-proof.txt')
if not p.exists():p.write_text('just-NAS Linux guest persistence proof\\n')
class H(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  data=json.dumps(dict(os=pathlib.Path('/etc/os-release').read_text(),sha256=hashlib.sha256(p.read_bytes()).hexdigest(),boot_id=pathlib.Path('/proc/sys/kernel/random/boot_id').read_text().strip(),desktop=subprocess.getoutput('systemctl is-active gdm3'),dns=subprocess.getoutput('getent hosts example.com'))).encode();self.send_response(200);self.end_headers();self.wfile.write(data)
 def log_message(self,*a):pass
http.server.HTTPServer(('0.0.0.0',8080),H).serve_forever()
'''
 write('just-nas-proof.py',proof)
 write('just-nas-proof.service','[Unit]\nAfter=network-online.target\n[Service]\nExecStart=/usr/bin/python3 /usr/local/lib/just-nas-proof.py\nRestart=on-failure\n[Install]\nWantedBy=multi-user.target\n')
 conf={'autoinstall':{'version':1,'locale':'en_US.UTF-8','keyboard':{'layout':'us'},'identity':{'hostname':'just-nas-ubuntu','username':c['user'],'password':hashed},'source':{'id':'ubuntu-desktop-minimal'},'refresh-installer':{'update':False},'storage':{'layout':{'name':'direct'}},'updates':'security','shutdown':'poweroff','apt':{'fallback':'offline-install','geoip':False},'codecs':{'install':False},'drivers':{'install':False},'late-commands':['cp /cdrom/just-nas-proof.py /target/usr/local/lib/just-nas-proof.py','cp /cdrom/just-nas-proof.service /target/etc/systemd/system/just-nas-proof.service','curtin in-target -- systemctl enable just-nas-proof.service',"printf '[daemon]\\nAutomaticLoginEnable=true\\nAutomaticLogin=nas-test\\n' > /target/etc/gdm3/custom.conf"]}}
 write('autoinstall.yaml',json.dumps(conf,indent=2))
 for name in ['grub','loopback']:
  s=(O/(name+'-original.cfg')).read_text(encoding='utf-8').replace('timeout=30','timeout=1').replace(' ---',' autoinstall ---');write(name+'.cfg',s)
 docker("xorriso -indev /media/ubuntu-24.04.4-desktop-amd64.iso -outdev /out/ubuntu-test-unattended.iso -overwrite on -map /out/autoinstall.yaml /autoinstall.yaml -map /out/just-nas-proof.py /just-nas-proof.py -map /out/just-nas-proof.service /just-nas-proof.service -map /out/grub.cfg /boot/grub/grub.cfg -map /out/loopback.cfg /boot/grub/loopback.cfg -boot_image any replay -commit -end")
elif sys.argv[1]=='windows':
 # Complete standard Windows image; only unattended answers and test proof added.
 password=c['password']
 xml=f'''<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend">
<settings pass="windowsPE"><component name="Microsoft-Windows-International-Core-WinPE" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS"><SetupUILanguage><UILanguage>en-US</UILanguage></SetupUILanguage><InputLocale>0409:00000409</InputLocale><SystemLocale>en-US</SystemLocale><UILanguage>en-US</UILanguage><UserLocale>en-US</UserLocale></component>
<component name="Microsoft-Windows-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS"><DiskConfiguration><Disk xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" wcm:action="add"><DiskID>0</DiskID><WillWipeDisk>true</WillWipeDisk><CreatePartitions><CreatePartition wcm:action="add"><Order>1</Order><Type>EFI</Type><Size>100</Size></CreatePartition><CreatePartition wcm:action="add"><Order>2</Order><Type>MSR</Type><Size>16</Size></CreatePartition><CreatePartition wcm:action="add"><Order>3</Order><Type>Primary</Type><Extend>true</Extend></CreatePartition></CreatePartitions><ModifyPartitions><ModifyPartition wcm:action="add"><Order>1</Order><PartitionID>1</PartitionID><Format>FAT32</Format><Label>System</Label></ModifyPartition><ModifyPartition wcm:action="add"><Order>2</Order><PartitionID>3</PartitionID><Format>NTFS</Format><Label>Windows</Label><Letter>C</Letter></ModifyPartition></ModifyPartitions></Disk></DiskConfiguration><ImageInstall><OSImage><InstallFrom><MetaData xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" wcm:action="add"><Key>/IMAGE/INDEX</Key><Value>3</Value></MetaData></InstallFrom><InstallTo><DiskID>0</DiskID><PartitionID>3</PartitionID></InstallTo><WillShowUI>OnError</WillShowUI></OSImage></ImageInstall><UserData><AcceptEula>true</AcceptEula><FullName>NAS test</FullName><Organization>just-NAS testing</Organization></UserData></component></settings>
<settings pass="specialize"><component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS"><ComputerName>JUSTNAS-WIN10</ComputerName><TimeZone>Taipei Standard Time</TimeZone></component></settings>
<settings pass="oobeSystem"><component name="Microsoft-Windows-International-Core" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS"><InputLocale>0409:00000409</InputLocale><SystemLocale>en-US</SystemLocale><UILanguage>en-US</UILanguage><UserLocale>en-US</UserLocale></component><component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS"><OOBE><HideEULAPage>true</HideEULAPage><HideOnlineAccountScreens>true</HideOnlineAccountScreens><HideWirelessSetupInOOBE>true</HideWirelessSetupInOOBE><ProtectYourPC>3</ProtectYourPC></OOBE><UserAccounts><LocalAccounts><LocalAccount xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" wcm:action="add"><Name>nas-test</Name><Group>Administrators</Group><Password><Value>{password}</Value><PlainText>true</PlainText></Password></LocalAccount></LocalAccounts></UserAccounts><AutoLogon><Username>nas-test</Username><Enabled>true</Enabled><LogonCount>12</LogonCount><Password><Value>{password}</Value><PlainText>true</PlainText></Password></AutoLogon></component></settings></unattend>'''
 write('Autounattend.xml',xml.replace('en-US','zh-TW').replace('<AcceptEula>','<ProductKey><WillShowUI>Never</WillShowUI></ProductKey><AcceptEula>'))
 write('proof.ps1',r'''$ErrorActionPreference='Stop'
$p='C:\Users\Public\just-nas-proof.txt'
if(!(Test-Path $p)){[IO.File]::WriteAllText($p,"just-NAS Windows guest persistence proof`n")}
$h=New-Object Net.HttpListener;$h.Prefixes.Add('http://+:8080/');$h.Start()
while($h.IsListening){$c=$h.GetContext();$v=@{os=(Get-CimInstance Win32_OperatingSystem).Caption;sha256=(Get-FileHash $p -Algorithm SHA256).Hash.ToLower();boot_id=(Get-CimInstance Win32_OperatingSystem).LastBootUpTime.ToString('o');desktop=@(Get-Process explorer -ErrorAction SilentlyContinue).Count;dns=@(Resolve-DnsName example.com -ErrorAction SilentlyContinue | Select-Object -ExpandProperty IPAddress)}|ConvertTo-Json -Compress;$b=[Text.Encoding]::UTF8.GetBytes($v);$c.Response.OutputStream.Write($b,0,$b.Length);$c.Response.Close()}
''')
 write('SetupComplete.cmd',r'''@echo off
powershell -NoProfile -Command "New-NetFirewallRule -DisplayName 'just-NAS test proof' -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow; $a=New-ScheduledTaskAction -Execute 'powershell.exe' -Argument '-NoProfile -ExecutionPolicy Bypass -File C:\JustNAS-Test\proof.ps1'; Register-ScheduledTask -TaskName 'JustNAS-Test-Proof' -Action $a -Trigger (New-ScheduledTaskTrigger -AtStartup) -User SYSTEM -RunLevel Highest -Force; Start-ScheduledTask -TaskName 'JustNAS-Test-Proof'"
exit /b 0
'''.replace('\n','\r\n'))
 # Shell single quotes deliberately keep Microsoft's literal $OEM$ directory.
 extract="" if '--repack' in sys.argv else "apk add --no-cache 7zip >/dev/null; mkdir -p /out/windows-tree; 7z x -y /media/Windows.iso -o/out/windows-tree >/dev/null; "
 docker(extract+"cp /out/Autounattend.xml /out/windows-tree/Autounattend.xml; mkdir -p '/out/windows-tree/sources/$OEM$/$$/Setup/Scripts' '/out/windows-tree/sources/$OEM$/$1/JustNAS-Test'; cp /out/SetupComplete.cmd '/out/windows-tree/sources/$OEM$/$$/Setup/Scripts/SetupComplete.cmd'; cp /out/proof.ps1 '/out/windows-tree/sources/$OEM$/$1/JustNAS-Test/proof.ps1'; test -f /out/windows-tree/efi/microsoft/boot/efisys_noprompt.bin; xorriso -as mkisofs -iso-level 3 -J -joliet-long -r -V WIN10_TEST -b boot/etfsboot.com -no-emul-boot -boot-load-size 8 -eltorito-alt-boot -e efi/microsoft/boot/efisys_noprompt.bin -no-emul-boot -o /out/windows10-test-unattended.iso /out/windows-tree")
else:raise SystemExit('ubuntu or windows required')
print('PASS private test-media copy created; originals unchanged')
