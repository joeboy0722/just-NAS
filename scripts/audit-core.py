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
