"""Local-only tests; never connects to NAS or Windows feature management."""
import importlib.util,io,pathlib,tarfile,tempfile,unittest,subprocess,sys
R=pathlib.Path(__file__).resolve().parents[1]
def load(name,file):
 s=importlib.util.spec_from_file_location(name,R/'scripts'/file);m=importlib.util.module_from_spec(s);s.loader.exec_module(m);return m
builder=load('nasbuild','build-on-nas.py');audit=load('auditengine','audit-optional-runtime.py')
kernel=load('kernel_tree','kernel_tree.py')
class BuildTests(unittest.TestCase):
 def test_source_boundary(self):
  for n in ('secrets/key','output/guest.qcow2','test-credentials.json','.env','notes/密碼.txt','x/private.pem','x/disk.vdi'):self.assertFalse(builder.allowed_source(n),n)
  self.assertTrue(builder.allowed_source('internal/extensions/runtime.go'))
  self.assertTrue(builder.allowed_source('internal/api/password.go'))
 def test_license_archive_has_nested_module_boundary(self):
  data=io.BytesIO()
  with tarfile.open(fileobj=data,mode='w') as archive:builder.add_artifact_module_boundary(archive)
  data.seek(0)
  with tarfile.open(fileobj=data,mode='r') as archive:self.assertEqual(archive.extractfile('output/go.mod').read(),b'module just-nas.local/build-artifacts\n')
 def test_preview_does_not_need_nas(self):
  r=subprocess.run([sys.executable,str(R/'scripts/build-on-nas.py')],capture_output=True,text=True,check=True);self.assertIn('PREVIEW ONLY',r.stdout)
 def test_gate_before_any_connection(self):
  with tempfile.TemporaryDirectory() as t:
   p=pathlib.Path(t)/'e.json';p.write_text('{"desktop":true,"disk_boot":false,"normal_shutdown":true}')
   r=subprocess.run([sys.executable,str(R/'scripts/build-on-nas.py'),'--execute','--identity','none','--known-hosts','none','--pool-guid','123','--tcg-evidence',str(p)],capture_output=True,text=True)
   self.assertNotEqual(r.returncode,0);self.assertIn('acceptance gate',r.stderr)
 def test_explicit_incomplete_override_still_requires_shutdown(self):
  evidence={'environment':'nas','outcome':'interrupted_by_user','machine_id':'a'*32,'desktop':False,'disk_boot':False,'normal_shutdown':True}
  with self.assertRaises(RuntimeError):builder.check_tcg_gate(evidence)
  self.assertEqual(builder.check_tcg_gate(evidence,True),'interrupted_by_user')
  evidence['normal_shutdown']=False
  with self.assertRaises(RuntimeError):builder.check_tcg_gate(evidence,True)
 def test_actual_kernel_identity_needs_no_split_inventory(self):
  with tempfile.TemporaryDirectory() as t:
   root=pathlib.Path(t);(root/'modules/firmware').mkdir(parents=True);(root/'modules/6.18.58-0-lts').mkdir()
   self.assertEqual(kernel.kernel_version(root),'6.18.58-0-lts')
   (root/'modules/6.19.0-other').mkdir()
   with self.assertRaises(ValueError):kernel.kernel_version(root)
 def test_core_rejects_payload_but_keeps_shared_libraries(self):
  with tempfile.TemporaryDirectory() as t:
   root=pathlib.Path(t);apks=root/'apks';apks.mkdir();files=root/'files';files.write_text('');overlay=root/'overlay.tgz'
   def tar(name):
    with tarfile.open(overlay,'w:gz') as f:f.addfile(tarfile.TarInfo(name),io.BytesIO())
   tar('usr/share/licenses/just-nas/third-party/licenses/restic/restic-0.18.1/docker');self.assertEqual(audit.check(apks,overlay,files)['result'],'PASS')
   tar('usr/lib/libseccomp.so');(apks/'libseccomp-2.5.5-r1.apk').touch();self.assertEqual(audit.check(apks,overlay,files)['result'],'PASS')
   for n in ('docker-cli-29.0.0-r0.apk','containerd-2.1.0-r0.apk','runc-1.3.0-r0.apk'):
    p=apks/n;p.touch()
    with self.assertRaises(ValueError):audit.check(apks,overlay,files)
    p.unlink()
   tar('etc/runlevels/default/docker')
   with self.assertRaises(ValueError):audit.check(apks,overlay,files)
   tar('usr/lib/libseccomp.so');files.write_text("'/optional/container-engine.jnpkg'")
   with self.assertRaises(ValueError):audit.check(apks,overlay,files)
if __name__=='__main__':unittest.main()
