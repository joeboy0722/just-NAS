"""Produce exactly five core release assets. No network publishing or private files."""
import argparse,pathlib,hashlib,json,subprocess,tarfile,io,shutil,re
R=pathlib.Path(__file__).resolve().parents[1]
def sha(p):
 h=hashlib.sha256()
 with p.open('rb') as f:
  while b:=f.read(1024**2):h.update(b)
 return h.hexdigest()
def git(*args):return subprocess.check_output(['git','-c','safe.directory='+R.as_posix(),*args],cwd=R).decode().strip()
def main():
 p=argparse.ArgumentParser();p.add_argument('--version',default='v2.1.0-rc2');p.add_argument('--iso',type=pathlib.Path,required=True);p.add_argument('--validation',type=pathlib.Path,required=True);a=p.parse_args();assert re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+-rc[0-9]+',a.version)
 assert not git('status','--porcelain'),'Commit sources before packaging';checks=json.loads(a.validation.read_text(encoding='utf-8'));assert checks and all(x['result']=='PASS' for x in checks)
 out=R/'output'/('release-'+a.version);work=R/'output'/('support-'+a.version);out.mkdir(exist_ok=True);work.mkdir(exist_ok=True)
 iso=out/('just-NAS-'+a.version+'-x86_64.iso');shutil.copyfile(a.iso,iso);shutil.copyfile(R/'LICENSE',out/'LICENSE')
 source=work/'just-NAS-source.tar.gz';subprocess.run(['git','-c','safe.directory='+R.as_posix(),'archive','--format=tar.gz','--prefix=just-NAS/','-o',str(source),'HEAD'],cwd=R,check=True)
 files={'source/just-NAS-source.tar.gz':source,'LICENSE':R/'LICENSE','THIRD-PARTY-NOTICES.md':R/'THIRD-PARTY-NOTICES.md','COMPLIANCE.md':R/'COMPLIANCE.md','docs/README.md':R/'README.md','docs/BOOT-CONSOLE.md':R/'BOOT-CONSOLE.md','docs/BOOT-VERIFICATION.md':R/'BOOT-VERIFICATION.md','docs/EXTENSIONS.md':R/'EXTENSIONS.md','licenses/core-licenses.tar.gz':R/'output/compliance/dist/just-NAS-v2.0.0-rc4-licenses.tar.gz','build/package-versions.txt':R/'output/package-versions.txt','build/aports-revision.txt':R/'output/aports-revision.txt','build/server-build-info.txt':R/'output/rc2-server-build-info.txt','verification/boot-results.json':a.validation,'verification/family-final-smoke.json':R/'output/vm-test/family-final-smoke.json'}
 for name in ['result.txt','core-transformation.json','runtime-commands.txt']:files['verification/iso-audit/'+name]=R/'output/core-iso-audit'/name
 for img in sorted((R/'output/vm-test/boot-rc2-screenshots').glob('*.png')):files['verification/screenshots/'+img.name]=img
 build={'version':a.version,'source_commit':git('rev-parse','HEAD'),'iso_sha256':sha(iso),'iso_bytes':iso.stat().st_size,'server_binary_commit':next(l.split('vcs.revision=',1)[1].strip() for l in (R/'output/rc2-server-build-info.txt').read_text(encoding='utf-8-sig').splitlines() if 'vcs.revision=' in l),'private_credentials_included':False,'third_party_sources':'See top-level SOURCES.md; upstream sources remain available alongside rc4 and stores-v1'}
 metadata=work/'BUILD-INFO.json';metadata.write_text(json.dumps(build,indent=2)+'\n',encoding='utf-8');files['BUILD-INFO.json']=metadata
 manifest=work/'SHA256SUMS';manifest.write_text(''.join(sha(path)+'  '+name+'\n' for name,path in sorted(files.items())),encoding='utf-8');files['SHA256SUMS']=manifest
 support=out/('just-NAS-'+a.version+'-support.tar.gz');epoch=int(git('show','-s','--format=%ct','HEAD'))
 with tarfile.open(support,'w:gz') as archive:
  for name,path in sorted(files.items()):
   assert path.is_file();info=tarfile.TarInfo(name);info.size=path.stat().st_size;info.mode=0o644;info.mtime=epoch
   with path.open('rb') as f:archive.addfile(info,f)
 # Source retrieval remains directly discoverable, not hidden only in an archive.
 sums=(R/'output/SHA256SUMS').read_text(encoding='utf-8');parts=[l for l in sums.splitlines() if '.part-' in l]
 links='\n'.join('- ['+l.split('  ',1)[1]+'](https://github.com/joeboy0722/just-NAS/releases/download/v2.0.0-rc4/'+l.split('  ',1)[1]+') — SHA256 `'+l.split('  ',1)[0]+'`' for l in parts)
 (out/'SOURCES.md').write_text('# Licenses and corresponding sources\n\njust-NAS project code and documentation: MIT, see LICENSE. The ISO and optional packages contain separately licensed third-party components; they are not collectively MIT.\n\n'+support.name+' contains exact project source, BUILD-INFO.json, complete collected core licenses, third-party notices, package versions, tests and instructions. Source commit: `'+build['source_commit']+'`.\n\nUnchanged kernel, firmware, core APK and Go sources reuse the verified rc4 source collection. Download all seven parts, verify hashes and concatenate in numeric order, then decompress tar.zst. Keep all source assets publicly available while distributing binaries.\n\n'+links+'\n\n[Reconstruction instructions](https://github.com/joeboy0722/just-NAS/releases/download/v2.0.0-rc4/SOURCES.md) · [part manifest](https://github.com/joeboy0722/just-NAS/releases/download/v2.0.0-rc4/SOURCE-PARTS.json)\n\n[Optional runtime sources](https://github.com/joeboy0722/just-NAS/releases/download/stores-v1/runtime-corresponding-sources.tar.zst) and [signed optional bundles](https://github.com/joeboy0722/just-NAS/releases/tag/stores-v1) remain available unchanged.\n',encoding='utf-8',newline='\n')
 (out/'SHA256SUMS').write_text(''.join(sha(f)+'  '+f.name+'\n' for f in [iso,out/'LICENSE',out/'SOURCES.md',support]),encoding='utf-8',newline='\n')
 assert len(list(out.iterdir()))==5,'Release directory must contain exactly five assets'
 with tarfile.open(support) as archive:
  names=archive.getnames();assert len(names)==len(set(names));assert 'source/just-NAS-source.tar.gz' in names and 'licenses/core-licenses.tar.gz' in names
  for name in names:assert not name.startswith('/') and '..' not in pathlib.PurePosixPath(name).parts
 print('PASS five release assets:',out)
if __name__=='__main__':main()
