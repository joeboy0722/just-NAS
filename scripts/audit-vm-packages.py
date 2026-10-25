"""Verify package signatures/payloads and every offline APK's source coverage."""
import json, pathlib, subprocess, tarfile

R = pathlib.Path(__file__).resolve().parents[1]

def audit():
    subprocess.run(['go', 'run', './cmd/nas-package-check', 'assets/store-keys/official.pub',
                    'output/vm-store-packages/*.jnpkg'], cwd=R, check=True)
    known = {}
    for name in ['compliance/iso/packages.json', 'vm-compliance/packages.json']:
        for p in json.loads((R/'output'/name).read_text(encoding='utf-8')):
            known[(p['pkgname'], p['pkgver'], p.get('commit',''))] = p
    rows = []
    for f in sorted((R/'output/vm-runtime-payload/apks').glob('*.apk')):
        with tarfile.open(f, 'r:gz', ignore_zeros=True) as t:
            p = dict(line.split(' = ',1) for line in t.extractfile('.PKGINFO').read().decode().splitlines() if ' = ' in line)
        key = (p['pkgname'], p['pkgver'], p.get('commit',''))
        assert key in known, 'Missing exact corresponding source: '+str(key)
        rows.append({'file':f.name, 'name':p['pkgname'], 'version':p['pkgver'],
                     'commit':p.get('commit',''), 'origin':p['origin'], 'license':p['license']})
    assert rows
    (R/'output/vm-test/runtime-apk-source-coverage.json').write_text(
        json.dumps({'result':'PASS', 'apk_count':len(rows), 'packages':rows}, indent=2), encoding='utf-8')
    print('PASS all',len(rows),'offline APKs match exact collected new or retained core sources',flush=True)

if __name__=='__main__': audit()
