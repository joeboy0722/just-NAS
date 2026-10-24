#!/usr/bin/env python3
"""Run BEFORE mksquashfs/signing. Explicit hardware groups; preserve every other path."""
import hashlib,json,os,pathlib,shutil,subprocess,sys

def classify(name):
    parts=name.split('/')
    if name.startswith('modules/firmware/'):
        f=name[len('modules/firmware/'):]
        if f.startswith('nvidia/'): return 'gpu-nvidia'
        if f.startswith(('amdgpu/','radeon/')): return 'gpu-amd'
        if f.startswith(('i915/','xe/')): return 'gpu-intel'
        if f.startswith(('cirrus/','intel/sof','intel/avs/','ess/','yamaha/','turtlebeach/','ctefx','ctspeq','emu/','sb16/','cs42l43')): return 'audio'
        if f.startswith(('amdnpu/','intel/vpu/')): return 'ai-accelerators'
        if f.startswith(('dvb-','v4l-','vicam/','go7007/')): return 'media'
    if '/kernel/sound/' in name or '/kernel/drivers/soundwire/' in name: return 'audio'
    if '/kernel/drivers/media/' in name: return 'media'
    if '/kernel/drivers/accel/' in name: return 'ai-accelerators'
    for prefix,group in [('amd/','gpu-amd'),('radeon/','gpu-amd'),('i915/','gpu-intel'),('xe/','gpu-intel'),('nouveau/','gpu-nvidia')]:
        if '/kernel/drivers/gpu/drm/'+prefix in name:return group
    return None

def split(root,out):
    root=pathlib.Path(root).resolve();out=pathlib.Path(out).resolve()
    assert (root/'modules').is_dir() and not out.is_relative_to(root)
    versions=[p.name for p in (root/'modules').iterdir() if p.is_dir() and p.name!='firmware'];assert len(versions)==1
    moved=[];kept=[]
    for p in sorted(root.rglob('*')):
        if not p.is_file() and not p.is_symlink():continue
        n=p.relative_to(root).as_posix();group=classify(n)
        item={'path':n,'group':group,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()} if p.is_file() else {'path':n,'group':group,'link':os.readlink(p)}
        if group:
            dst=out/group/n;dst.parent.mkdir(parents=True,exist_ok=True)
            # Bundle regular files only; firmware aliases are dereferenced before removal.
            if p.is_symlink():shutil.copyfile(p,dst)
            else:shutil.copy2(p,dst)
            moved.append(item)
        else:kept.append(item)
    # Copy everything before unlinking, so aliases never lose their source halfway through.
    for i in moved:(root/i['path']).unlink()
    for i in kept:
        p=root/i['path']
        if p.is_symlink() and not p.exists():raise RuntimeError('Retained link lost target: '+i['path'])
    for prefix in ['kernel/drivers/net/','kernel/drivers/ata/','kernel/drivers/scsi/','kernel/drivers/usb/','kernel/drivers/nvme/']:
        assert not any('/'+prefix in i['path'] for i in moved),prefix
    # Regenerate dependency indexes after removal. No optional module may be needed by a retained module.
    dep=root/'modules'/versions[0]/'modules.dep'
    removed={i['path'].split('/'+versions[0]+'/')[-1] for i in moved if '/'+versions[0]+'/' in i['path']}
    if dep.exists():
        for line in dep.read_text().splitlines():
            module,_,deps=line.partition(':')
            if module not in removed and any(d in removed for d in deps.split()):raise RuntimeError('Retained module depends on removed module: '+line)
    fake=out/'depmod-root';(fake/'lib').mkdir(parents=True,exist_ok=True)
    (fake/'lib/modules').symlink_to(root/'modules',target_is_directory=True)
    subprocess.run(['depmod','-b',str(fake),versions[0]],check=True)
    (fake/'lib/modules').unlink();(fake/'lib').rmdir();fake.rmdir()
    out.mkdir(parents=True,exist_ok=True)
    (out/'split-inventory.json').write_text(json.dumps({'kernel':versions[0],'moved':moved,'retained':kept},indent=2))
    print('Split optional files:',len(moved),'retained:',len(kept),flush=True)
if __name__=='__main__':split(sys.argv[1],sys.argv[2])
