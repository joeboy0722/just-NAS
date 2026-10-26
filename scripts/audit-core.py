#!/usr/bin/env python3
import hashlib,json,pathlib,subprocess,sys
root=pathlib.Path(sys.argv[1]);report=pathlib.Path(sys.argv[2]);known=set(pathlib.Path('/builder/compliance/runtime-apks.txt').read_text().splitlines());actual=set((report/'runtime-apks.txt').read_text().splitlines())
assert actual<=known, 'New runtime APK lacks matching corresponding source: '+str(actual-known)
removed=known-actual
assert all(n.startswith(('openssh-','libedit-')) for n in removed), 'Unexpected core package removal: '+str(removed)
# Verify source-to-core transformation against the untouched rc4 payload, not a new unaudited baseline.
baseline=pathlib.Path('/builder/output/alpine-nas-full.iso');assert baseline.exists()
subprocess.run(['xorriso','-osirrox','on','-indev',str(baseline),'-extract','/boot/modloop-lts',str(root/'baseline-modloop'),'-extract','/boot/vmlinuz-lts',str(root/'baseline-kernel')],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
identity=json.loads(pathlib.Path('/builder/compliance/boot-identity.json').read_text())
assert hashlib.sha256((root/'baseline-modloop').read_bytes()).hexdigest()==identity['modloop_sha256']
subprocess.run(['xorriso','-osirrox','on','-indev',sys.argv[3] if len(sys.argv)>3 else '/builder/output/alpine-nas-stores.iso','-extract','/boot/vmlinuz-lts',str(root/'core-kernel')],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
assert (root/'core-kernel').read_bytes()==(root/'baseline-kernel').read_bytes(), 'Kernel source collection changed'
subprocess.run(['unsquashfs','-no-progress','-d',str(root/'baseline-tree'),str(root/'baseline-modloop')],check=True,stdout=subprocess.DEVNULL)
subprocess.run(['unsquashfs','-no-progress','-d',str(root/'core-tree'),str(root/'modloop-lts')],check=True,stdout=subprocess.DEVNULL)
base=root/'baseline-tree';core=root/'core-tree'
sys.path.insert(0,'/builder/scripts');import importlib.util
spec=importlib.util.spec_from_file_location('splitmod','/builder/scripts/split-modloop.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module);classify=module.classify
removed_files=[]
for p in base.rglob('*'):
 if not p.is_file():continue
 n=p.relative_to(base).as_posix();q=core/n
 if classify(n):
  assert not q.exists(),'Optional hardware remains in core: '+n;removed_files.append(n)
 elif pathlib.Path(n).name.startswith('modules.'):
  continue # depmod indexes are deliberately regenerated.
 else:assert q.is_file() and hashlib.sha256(p.read_bytes()).digest()==hashlib.sha256(q.read_bytes()).digest(),'Retained payload changed: '+n
for p in core.rglob('*'):
 if p.is_file():assert (base/p.relative_to(core)).exists(),'Unexpected file added to core payload'
(report/'core-transformation.json').write_text(json.dumps({'removed_apks':sorted(removed),'removed_driver_files':len(removed_files),'baseline_modloop_sha256':identity['modloop_sha256'],'retained_payload':'byte-identical except regenerated depmod indexes'},indent=2))

# Verify the new modloop against the signature actually embedded in its initramfs.
iso='/builder/output/alpine-nas-stores.iso'
from kernel_tree import kernel_version
actual_kernel=kernel_version(core)
subprocess.run(['xorriso','-osirrox','on','-indev',iso,'-extract','/boot/initramfs-lts',str(root/'core-initramfs'),'-extract','/boot/config-'+actual_kernel,str(root/'core-config')],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
initrd=root/'core-initrd';initrd.mkdir()
subprocess.run(['sh','-c','gzip -dc "$1" | (cd "$2" && cpio -idmu)','sh',str(root/'core-initramfs'),str(initrd)],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
verified=False
for sig in initrd.rglob('modloop-lts.SIGN.RSA.*'):
 keyname=sig.name.split('.SIGN.RSA.',1)[1]
 for key in initrd.rglob(keyname):
  if key.name.endswith('.pub'):
   r=subprocess.run(['openssl','dgst','-sha1','-verify',str(key),'-signature',str(sig),str(root/'modloop-lts')],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
   if r.returncode==0:verified=True
assert verified,'Core modloop does not have a valid embedded signature'
configuration=(root/'core-config').read_text()
assert any(v in configuration for v in ['CONFIG_DRM_SIMPLEDRM=y','CONFIG_FB_EFI=y','CONFIG_FB_VESA=y']),'Basic firmware display fallback is not built into the kernel'
print('PASS: retained hardware bytes, matching kernel sources, signed modloop and built-in basic display')

# A retained USB/NIC/storage/platform module must not lose firmware through a vendor-name split.
import fnmatch
modules=[str(p) for p in core.rglob('*.ko')]
patterns=set()
for offset in range(0,len(modules),80):
 result=subprocess.run(['modinfo','-F','firmware',*modules[offset:offset+80]],check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
 patterns.update(line.strip() for line in result.stdout.splitlines() if line.strip())
conflicts=[]
for name in removed_files:
 if not name.startswith('modules/firmware/'):continue
 fw=name.removeprefix('modules/firmware/')
 for suffix in ['.zst','.xz','.gz']:
  if fw.endswith(suffix):fw=fw[:-len(suffix)];break
 if any(fnmatch.fnmatchcase(fw,pattern) for pattern in patterns):conflicts.append(name)
assert not conflicts,'Retained hardware still requires optional firmware: '+str(conflicts)
print('PASS: all removed firmware is unreferenced by retained hardware modules')
