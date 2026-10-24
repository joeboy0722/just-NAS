"""Create ISO notice tree and independently downloadable corresponding-source parts."""
from pathlib import Path
import csv, hashlib, json, os, re, shutil, subprocess, tarfile

WORK=Path('/work');TREE=WORK/'source-tree';ISO=WORK/'iso';DIST=WORK/'dist'
assert (WORK/'sources-collected.ok').is_file() and (WORK/'go-sources-collected.ok').is_file()
ISO.mkdir(exist_ok=True);DIST.mkdir(exist_ok=True)

def sha(path):
    with path.open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()

def write(path,text):
    path.parent.mkdir(parents=True,exist_ok=True);path.write_text(text,encoding='utf8',newline='\n')

packages=json.loads((WORK/'packages.json').read_text())
sources=json.loads((WORK/'source-inventory.json').read_text())
# Preserve the exact source scripts, not redundant compiled initramfs programs.
initdir=TREE/'initramfs-distributed-files'
if initdir.is_dir():
    write(WORK/'initramfs-files.txt','\n'.join(sorted(str(f.relative_to(initdir)) for f in initdir.rglob('*')))+'\n')
    init=initdir/'init'
    if init.is_file():shutil.copyfile(init,TREE/'initramfs-init')
    assert initdir.resolve()==Path('/work/source-tree/initramfs-distributed-files')
    shutil.rmtree(initdir)

for record in sources:
    origin=record['origin'];target=WORK/'licenses'/origin
    target.mkdir(exist_ok=True)
    write(target/'PACKAGE.txt',json.dumps(record,indent=2)+'\n')
    if not any(s['uri'].startswith(('http:','https:','ftp:')) for s in record['sources']):
        shutil.copytree(TREE/'recipes'/origin,target/'distributed-local-sources',dirs_exist_ok=True)

shutil.copytree(WORK/'licenses',ISO/'licenses',dirs_exist_ok=True)
metadata=['packages.json','source-inventory.json','go-modules.json','go-toolchains.json','go-build-info.json','boot-identity.json','initramfs-files.txt']
for name in metadata:
    shutil.copyfile(WORK/name,ISO/name)
    shutil.copyfile(WORK/name,TREE/name)
with (ISO/'packages.csv').open('w',encoding='utf8',newline='') as f:
    fields=['pkgname','pkgver','license','origin','commit','distribution']
    writer=csv.DictWriter(f,fieldnames=fields,extrasaction='ignore');writer.writeheader();writer.writerows(packages)
write(ISO/'runtime-apks.txt',''.join(sorted(p['pkgname']+'-'+p['pkgver']+'.apk\n' for p in packages if p.get('distribution')!='boot')))
# Inventory firmware paths from the exact input modloop, independently of package names.
import tempfile
with tempfile.TemporaryDirectory() as temp:
    modloop=Path(temp)/'modloop'
    subprocess.run(['xorriso','-osirrox','on','-indev','/input.iso','-extract','/boot/modloop-lts',str(modloop)],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    paths=subprocess.check_output(['unsquashfs','-l',str(modloop)]).decode().splitlines()
    firmware=[p.split('/modules/firmware/',1)[1] for p in paths if '/modules/firmware/' in p]
write(ISO/'firmware-files.txt','\n'.join(firmware)+'\n')
shutil.copyfile(ISO/'firmware-files.txt',TREE/'firmware-files.txt')

guide='''# Corresponding source for just-NAS v2.0.0-rc4

MIT applies only to just-NAS project code. Each bundled upstream component retains
its own license, notices, source obligations and exceptions.

## Complete downloads, alongside the ISO

https://github.com/joeboy0722/just-NAS/releases/tag/v2.0.0-rc4

Download every just-NAS-v2.0.0-rc4-sources.tar.zst.part-* file and SHA256SUMS.
These are consecutive binary parts of ONE zstd-compressed tar stream, not separate
archives. Verify each part against SHA256SUMS before combining them in name order.

Linux/macOS with zstd installed:

    cat just-NAS-v2.0.0-rc4-sources.tar.zst.part-* | zstd -d | tar -xf -

Windows PowerShell (combines the parts without loading the archive into RAM):

    $combined = [IO.File]::Create("just-NAS-v2.0.0-rc4-sources.tar.zst")
    try {
      Get-ChildItem just-NAS-v2.0.0-rc4-sources.tar.zst.part-* | Sort-Object Name | ForEach-Object {
        $part = [IO.File]::OpenRead($_.FullName)
        try { $part.CopyTo($combined) } finally { $part.Dispose() }
      }
    } finally { $combined.Dispose() }

Then decompress the combined .tar.zst with zstd or an archive tool supporting zstd.

## Contents and reconstruction

- recipes/<origin>/: original APKBUILD, patches, local files and APORTS-COMMIT,
  taken from the commit recorded by the APK.
- upstream/<origin>/: upstream source archives, patches and firmware distribution
  files, checked against the SHA512/SHA256 values from that precise APKBUILD.
- source-inventory.json: exact recipe commits, source URLs and verified checksums.
- packages.json: actual ISO offline APKs plus boot component metadata.
- go-module-proxy/: exact Restic module .zip/.mod/.info files; module h1 checksums
  verified against Go build information and the Go module verification mechanism.
- upstream/go/: exact Go source distributions for the shipped Go runtimes,
  checked against the Go project's published SHA256 values.
- initramfs-init and initramfs-files.txt: distributed boot script and file inventory.

For Alpine packages, use the original x86_64 Alpine 3.24 build environment and
abuild; set SRCDEST to upstream/<origin> and run the recipe under recipes/<origin>.
The exact Alpine recipe, its local patches and checksum data are retained. Consult
the APKBUILD for build dependencies and any package-specific steps. Source packages
are not relabeled MIT; their original licensing remains in force.

For Restic, unpack its upstream source, use its recorded Go compiler, point GOPROXY
to file:///absolute/path/go-module-proxy, and follow its preserved Alpine build recipe.
go-build-info.json and go-modules.json identify the compiled dependency versions.
For just-NAS itself, use the GitHub v2.0.0-rc4 source tag and the repository build
scripts; the ISO is assembled from the Alpine kernel/boot sections and APK repository.
Rebuilding need not produce an identical hash: timestamps and signatures can differ.

Firmware upstream distributions, WHENCE and vendor license notices are preserved,
including Raspberry Pi Bluetooth/Broadcom supplemental sources used by Alpine.
firmware-files.txt inventories the paths actually included in the modloop; it is
not a replacement for the vendor-specific notices in the licenses directory.

OpenZFS is a separate dynamically loaded zfs.ko, not built into the Linux kernel.
OpenZFS's published position permits distributing a binary module; FSF has a
different view on combining ZFS with Linux. These documents/source files do not
resolve that disagreement or constitute a legal opinion:
https://openzfs.github.io/openzfs-docs/License.html
https://www.fsf.org/licensing/zfs-and-linux

Keep source downloads available while distributing the ISO. Do not remove source
assets and leave the binary-only release available. No private build signing key,
NAS credentials, user files or VM disks are included in this source collection.
'''
write(TREE/'README-SOURCES.md',guide)
write(ISO/'SOURCES.md',guide)
write(DIST/'SOURCES.md',guide)

# Stable file order and ownership; archive timestamps are not a reproducible-build claim.
tar=subprocess.Popen(['tar','--sort=name','--owner=0','--group=0','-cf','-','-C',str(TREE),'.'],stdout=subprocess.PIPE)
zstd=subprocess.Popen(['zstd','-T2','-3','-q','-c'],stdin=tar.stdout,stdout=subprocess.PIPE)
tar.stdout.close()
parts=[];whole=hashlib.sha256();limit=256*1024*1024;index=0
while True:
    data=zstd.stdout.read(1024*1024)
    if not data:break
    index+=1;name=f'just-NAS-v2.0.0-rc4-sources.tar.zst.part-{index:03d}'
    target=DIST/name;size=0;digest=hashlib.sha256()
    with target.open('wb') as f:
        while data:
            f.write(data);size+=len(data);digest.update(data);whole.update(data)
            if size>=limit:break
            data=zstd.stdout.read(min(1024*1024,limit-size))
    parts.append({'name':name,'bytes':size,'sha256':digest.hexdigest()})
    print(f'Created source part {index}: {size} bytes',flush=True)
if zstd.wait()!=0 or tar.wait()!=0:raise RuntimeError('Source archive creation failed')
partinfo={'format':'concatenated tar.zst','assembled_sha256':whole.hexdigest(),'parts':parts}
write(DIST/'SOURCE-PARTS.json',json.dumps(partinfo,indent=2)+'\n')
shutil.copyfile(DIST/'SOURCE-PARTS.json',ISO/'SOURCE-PARTS.json')
write(ISO/'READY','just-NAS licensing data schema 1\n')
manifest=''.join(f'{sha(p)}  {p.relative_to(ISO).as_posix()}\n' for p in sorted(ISO.rglob('*')) if p.is_file() and p.name!='files.sha256')
write(ISO/'files.sha256',manifest)
with tarfile.open(DIST/'just-NAS-v2.0.0-rc4-licenses.tar.gz','w:gz') as archive:
    archive.add(WORK/'go.mod',arcname='go.mod')
    archive.add(ISO,arcname='iso')
write(WORK/'packaged.ok','PASS\n')
print(f'PASS {len(packages)} package records, {len(parts)} source parts, ISO license tree ready',flush=True)
