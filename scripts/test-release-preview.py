"""Local safety tests: never publishes or connects to a VM."""
import importlib.util,json,pathlib,tempfile,unittest
R=pathlib.Path(__file__).resolve().parents[1];sp=importlib.util.spec_from_file_location('preview',R/'scripts/release-preview.py');m=importlib.util.module_from_spec(sp);sp.loader.exec_module(m)
class ReleaseTests(unittest.TestCase):
 def report(self):return {'result':'PREVIEW','channel':'prerelease','checks':{k:'PASS' for k in ['windows_kvm_desktop','windows_iso_detached_disk_boot','windows_normal_shutdown','nas_self_build','nas_self_built_cold_boot','package_signatures','runtime_sources']},'deferred':['soak'],'known_limitations':['Linux desktop unverified']}
 def test_preview_never_claims_full_acceptance(self):
  with tempfile.TemporaryDirectory() as t:
   p=pathlib.Path(t)/'v.json';d=self.report();p.write_text(json.dumps(d));self.assertEqual(m.evidence(p)['result'],'PREVIEW')
   for key,value in [('result','PASS'),('deferred',[]),('known_limitations',[])]:
    d=self.report();d[key]=value;p.write_text(json.dumps(d))
    with self.assertRaises(AssertionError):m.evidence(p)
 def test_core_requires_online_install(self):
  with tempfile.TemporaryDirectory() as t:
   p=pathlib.Path(t)/'v.json';d=self.report();p.write_text(json.dumps(d))
   with self.assertRaises((AssertionError,KeyError)):m.evidence(p,True)
   d['checks']['online_install']='PASS';p.write_text(json.dumps(d));m.evidence(p,True)
 def test_unknown_assets_and_tampered_bytes_rejected(self):
  with tempfile.TemporaryDirectory() as t:
   p=pathlib.Path(t);names=['LICENSE','SOURCES.md','just-NAS-v2.1.0-rc4-x86_64.iso','just-NAS-v2.1.0-rc4-support.tar.gz']
   for n in names:(p/n).write_bytes(b'fixture')
   (p/'SHA256SUMS').write_text(''.join(m.sha(p/n)+'  '+n+'\n' for n in names));m.checked_files(p,True)
   (p/'private-credentials.json').write_text('{}')
   with self.assertRaises(AssertionError):m.checked_files(p,True)
   (p/'private-credentials.json').unlink();(p/names[-1]).write_bytes(b'changed')
   with self.assertRaises(AssertionError):m.checked_files(p,True)
if __name__=='__main__':unittest.main()
