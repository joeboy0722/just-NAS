"""Import-only negative cases. Never installs or stops an application/guest."""
import base64, copy, importlib.util, io, json, pathlib, tarfile

R = pathlib.Path(__file__).resolve().parents[1]
sp = importlib.util.spec_from_file_location('v', R/'scripts/verify-vm-app.py')
v = importlib.util.module_from_spec(sp); sp.loader.exec_module(v); v.ready()
from cryptography.hazmat.primitives import serialization
key = serialization.load_pem_private_key((R/'secrets/stores-ed25519.key').read_bytes(), None)
with tarfile.open(R/'output/vm-store-packages/vm-manager-1.0.0-rc1.jnpkg') as t:
    original = json.load(t.extractfile('package.json'))

def archive(envelope):
    raw = json.dumps(envelope, separators=(',', ':'), ensure_ascii=False).encode()
    out = io.BytesIO()
    with tarfile.open(fileobj=out, mode='w:gz') as t:
        info = tarfile.TarInfo('package.json'); info.size = len(raw)
        t.addfile(info, io.BytesIO(raw))
    return out.getvalue()

def signed(manifest):
    raw = json.dumps(manifest, separators=(',', ':'), ensure_ascii=False).encode()
    return {'manifest': manifest, 'signature': base64.b64encode(key.sign(raw)).decode()}

cases = []
bad = copy.deepcopy(original); bad['signature'] = base64.b64encode(bytes(64)).decode()
cases.append(('invalid official signature', archive(bad)))
bad = copy.deepcopy(original); bad['signature'] = ''
cases.append(('unsigned native application', archive(bad)))
for field, value in [('arch', 'aarch64'), ('min_nas_version', '99.0.0'), ('native', {'service': 'shell'})]:
    m = copy.deepcopy(original['manifest']); m[field] = value
    cases.append(('incompatible '+field, archive(signed(m))))
cases.append(('truncated compressed package', (R/'output/vm-store-packages/vm-manager-1.0.0-rc1.jnpkg').read_bytes()[:4096]))
results = []
for label, body in cases:
    r = v.s.post(v.BASE+'/api/apps/import', data=body,
                 headers={'Content-Type': 'application/octet-stream'}, timeout=60)
    assert r.status_code in (400, 409, 422), (label, r.status_code, r.text[:300])
    results.append({'check': label, 'result': 'PASS', 'http_status': r.status_code})
    print('PASS rejected', label, flush=True)
(R/'output/vm-test/package-rejections.json').write_text(json.dumps(results, indent=2), encoding='utf-8')
