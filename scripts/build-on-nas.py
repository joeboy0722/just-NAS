"""Explicit, gated administrator build transport; never calls local Docker or modifies Windows."""
import argparse,hashlib,io,json,pathlib,re,shlex,subprocess,tarfile,tempfile,uuid
R=pathlib.Path(__file__).resolve().parents[1]
def allowed_source(name):
 p=pathlib.PurePosixPath(name); n=p.name.lower()
 return not any(x in ('.git','output','secrets','.codex','.agents') for x in p.parts) and not n.startswith('.env') and not (n.startswith(('password','密碼')) and pathlib.PurePosixPath(n).suffix in ('.txt','.md','.json')) and not n.endswith(('.key','.pem','.iso','.vdi','.qcow2','.bundle','.pfx','.p12','-credentials.json'))
def run(args,**kw):return subprocess.run(args,check=True,**kw)
def add_artifact_module_boundary(archive):
 # License collections can contain complete upstream Go source trees. Keep
 # generated artifacts out of the project's go test/vet ./... traversal.
 data=b'module just-nas.local/build-artifacts\n'
 member=tarfile.TarInfo('output/go.mod');member.size=len(data);member.mode=0o644
 archive.addfile(member,io.BytesIO(data))

def check_tcg_gate(evidence, allow_incomplete=False):
 if evidence.get('environment') == 'nas' and all(evidence.get(k) is True for k in ('desktop','disk_boot','normal_shutdown')):return 'complete'
 if allow_incomplete and evidence.get('environment') == 'nas' and evidence.get('outcome') == 'interrupted_by_user' and evidence.get('normal_shutdown') is True and re.fullmatch(r'[a-f0-9]{32}',evidence.get('machine_id','')):return 'interrupted_by_user'
 raise RuntimeError('Current NAS TCG acceptance gate has not passed; an explicit incomplete-test override still requires a confirmed normal shutdown')

def main():
 a=argparse.ArgumentParser(description=__doc__);a.add_argument('--execute',action='store_true');a.add_argument('--allow-incomplete-tcg',action='store_true',help='User explicitly ended TCG testing; requires truthful interrupted_by_user evidence and normal shutdown');a.add_argument('--host',default='192.168.56.101');a.add_argument('--identity',type=pathlib.Path);a.add_argument('--known-hosts',type=pathlib.Path);a.add_argument('--pool-guid');a.add_argument('--tcg-evidence',type=pathlib.Path);v=a.parse_args()
 if not v.execute:
  print('PREVIEW ONLY: no NAS or Windows changes. Execution requires SSH identity, pinned known_hosts, pool GUID, and TCG acceptance JSON with desktop/disk_boot/normal_shutdown=true.');return
 if not re.fullmatch(r'[a-zA-Z0-9.-]+',v.host) or not v.identity or not v.known_hosts or not v.pool_guid or not v.pool_guid.isdigit() or not v.tcg_evidence:a.error('Missing or invalid explicit build prerequisites')
 evidence=json.loads(v.tcg_evidence.read_text(encoding='utf-8'))
 gate=check_tcg_gate(evidence,v.allow_incomplete_tcg)
 if gate != 'complete':print('WARNING: user-ended TCG test is incomplete, not a passed OS acceptance; continuing NAS build with guests normally stopped',flush=True)
 git=['git','-c','safe.directory='+R.as_posix()]
 run(git+['diff','--quiet','HEAD'],cwd=R)
 sha=subprocess.check_output(git+['rev-parse','HEAD'],cwd=R,text=True).strip()
 opts=['-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+str(v.known_hosts.resolve()),'-o','IdentitiesOnly=yes','-i',str(v.identity.resolve())]
 def ssh(command,**kw):return run(['ssh',*opts,'root@'+v.host,command],**kw)
 def scp(src,target):return run(['scp',*opts,str(src),'root@'+v.host+':'+target])
 rid=uuid.uuid4().hex; remote='/mnt/vmtest/.just-nas-build/runs/'+rid
 licenses=R/'output/compliance/iso';baseline=R/'output/alpine-nas-full.iso'
 if not (licenses/'READY').is_file() or not baseline.is_file():raise RuntimeError('Matching license collection and unchanged baseline ISO required')
 with tempfile.TemporaryDirectory(prefix='nas-build-') as t:
  archive=pathlib.Path(t)/'source.tar'
  with tarfile.open(archive,'w') as out:
   committed=subprocess.check_output(git+['archive','--format=tar','HEAD'],cwd=R)
   with tarfile.open(fileobj=io.BytesIO(committed),mode='r:') as source:
    for member in source:
     if member.isdir() or not allowed_source(member.name):continue
     if not member.isfile():raise RuntimeError('Refusing non-regular source: '+member.name)
     out.addfile(member,source.extractfile(member))
   add_artifact_module_boundary(out)
   out.add(licenses,arcname='output/compliance/iso');out.add(baseline,arcname='output/alpine-nas-full.iso')
  with archive.open('rb') as f:digest=hashlib.file_digest(f,'sha256').hexdigest()
  # Source is sent only after the remote pool identity and mount checks succeed.
  setup=(R/'scripts/build-on-nas.sh').read_text(encoding='utf-8').encode('utf-8')
  ssh('sh -s -- '+shlex.quote(v.pool_guid)+' '+rid,input=setup)
  scp(archive,remote+'/source.tar')
  command='cd '+shlex.quote(remote)+' && printf '+shlex.quote(digest+'  source.tar\n')+' | sha256sum -c - && tar -xf source.tar && rm source.tar && sh scripts/nas-build-worker.sh '+shlex.quote(remote)
  dest=R/'output/nas-self-build'/rid;dest.mkdir(parents=True,exist_ok=False)
  # Keep the log even if the remote build fails; no local-engine fallback.
  try:ssh(command+' > '+shlex.quote(remote+'/output/build.log')+' 2>&1')
  finally:
   try:run(['scp',*opts,'root@'+v.host+':'+remote+'/output/build.log',str(dest/'build.log')])
   except subprocess.CalledProcessError:print('Remote build log unavailable; preserve remote workspace for diagnosis')
 for n in ('just-NAS-nas-built-candidate-x86_64.iso','SHA256SUMS','builder-images.json','NAS-BUILD-PASS.txt','package-versions.txt'):
  run(['scp',*opts,'root@'+v.host+':'+remote+'/output/'+n,str(dest/n)])
 expected=(dest/'SHA256SUMS').read_text().split()[0]
 with (dest/'just-NAS-nas-built-candidate-x86_64.iso').open('rb') as f:assert hashlib.file_digest(f,'sha256').hexdigest()==expected
 (dest/'source-revision.json').write_text(json.dumps({'commit':sha,'source_archive_sha256':digest,'remote':remote,'vm_boot_verified':False},indent=2))
 print('PASS NAS-only build and transfer hash; candidate at',dest)
if __name__=='__main__':main()
