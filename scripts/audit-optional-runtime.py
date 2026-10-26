"""Reject optional engine payload in a finished core ISO (no extraction or execution)."""
import json,pathlib,re,tarfile,sys
BANNED = re.compile(r"^(docker(?:-.+)?|containerd(?:-.+)?|runc(?:-.+)?|buildkit(?:-.+)?|rootlesskit|slirp4netns|fuse-overlayfs)$")
BINARIES = {"docker","dockerd","docker-proxy","containerd","containerd-shim","containerd-shim-runc-v2","ctr","runc","buildkitd","buildctl"}
def check(root, overlay, files):
 bad=[]
 for p in pathlib.Path(root).rglob('*.apk'):
  name=p.name[:-4].rsplit('-',2)[0]
  if BANNED.fullmatch(name):bad.append('apk:'+p.name)
 with tarfile.open(overlay,'r:gz') as t:
  for member in t:
   n=member.name.removeprefix('./'); p=pathlib.PurePosixPath(n)
   if (p.name in BINARIES and p.parent.as_posix() in ("bin","sbin","usr/bin","usr/sbin","usr/local/bin","usr/local/sbin","usr/libexec/docker")) or n in ('usr/bin/go','usr/local/go/bin/go') or (n.startswith('etc/runlevels/') and p.name in ('docker','containerd')) or n.endswith('.jnpkg') or p.name=='image.tar':bad.append('overlay:'+n)
 for line in pathlib.Path(files).read_text(encoding='utf-8').splitlines():
  if re.search(r"(?:\.jnpkg|/image\.tar)'?$",line):bad.append('iso:'+line)
 if bad:raise ValueError('Optional container engine payload leaked into core: '+', '.join(bad))
 return {'result':'PASS','optional_engine_payload':'absent','shared_kernel_and_libraries':'retained; not classified by vendor or Docker use'}
if __name__=='__main__':
 result=check(*sys.argv[1:4]);pathlib.Path(sys.argv[4]).write_text(json.dumps(result,indent=2)+'\n');print('PASS: core has no Docker/containerd/runc/build tools, engine bundles or container images')
