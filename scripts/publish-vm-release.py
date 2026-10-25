"""Stage/validate VM store assets. Publishing requires real offline acceptance.

Never publishes a core ISO, OS installation medium, guest disk, or credential.
Existing releases and stores-v1 are immutable.
"""
import argparse, hashlib, json, os, pathlib, re, shutil, subprocess, tarfile
import requests

R = pathlib.Path(__file__).resolve().parents[1]
REPO = 'joeboy0722/just-NAS'
API = 'https://api.github.com/repos/'+REPO
CHECKS = '''linux_install linux_disk_boot linux_desktop linux_persistence
windows10_media windows_install windows_disk_boot windows_desktop windows_persistence
nat_dns internet port_forward port_conflict port_release resource_edit disk_growth
iso_attach_detach shutdown_timeout force_confirmation retain_restore offline_install
signature_rejection dependency_rejection interrupted_upload full_storage pool_missing
process_exit manager_restart update_rollback cold_boots soak nas_regression
go packages browser responsive'''.split()

def git(*args):
    return subprocess.check_output(['git', '-c', 'safe.directory='+R.as_posix(), *args], cwd=R, text=True).strip()

def sha(p):
    h = hashlib.sha256()
    with p.open('rb') as f:
        while b := f.read(1<<20): h.update(b)
    return h.hexdigest()

def acceptance():
    report = json.loads((R/'output/vm-test/offline-acceptance.json').read_text(encoding='utf-8'))
    assert report['result'] == 'PASS'
    for name in CHECKS:
        c = report['checks'][name]
        assert c['result'] == 'PASS', name
        p = (R/c['evidence']).resolve()
        p.relative_to((R/'output').resolve())
        assert p.is_file() and sha(p) == c['sha256'], name+' missing/changed evidence'
    assert report['cold_boot_count'] >= 3 and report['continuous_ready_seconds'] >= 1800
    assert report['kvm'] in ('PASS', 'PENDING_REAL_HARDWARE')
    return report

def stage(dest, report):
    assert not git('status', '--porcelain'), 'Commit tested sources before staging'
    subprocess.run(['python', str(R/'scripts/audit-repository.py')], cwd=R, check=True)
    subprocess.run(['python', str(R/'scripts/audit-vm-packages.py')], cwd=R, check=True)
    commit = git('rev-parse', 'HEAD')
    dest.mkdir(parents=True, exist_ok=True)
    names = ['qemu-runtime-11.0.3-r0.jnpkg', 'vm-manager-1.0.0-rc1.jnpkg', 'catalog.json']
    for name in names: shutil.copyfile(R/'output/vm-store-packages'/name, dest/name)
    catalog = json.loads((dest/'catalog.json').read_text(encoding='utf-8'))['catalog']
    assert catalog['abi'] == 'just-nas-stores-v2'
    for entry in catalog['entries']:
        if entry['id'] not in ('qemu-runtime', 'vm-manager'): continue
        p = dest/entry['url'].rsplit('/', 1)[-1]
        assert entry['url'].startswith('https://github.com/'+REPO+'/releases/download/stores-v2/')
        assert entry['bytes'] == p.stat().st_size and entry['sha256'] == sha(p)
    source = R/'output/vm-compliance/source-tree'
    inventory = json.loads((R/'output/vm-compliance/source-inventory.json').read_text())
    for origin in inventory:
        for item in origin['sources']:
            assert item['verified']
            p = source/item['path']
            assert p.is_file(), str(p)
            if item.get('sha256'): assert sha(p) == item['sha256']
            if item.get('checksum_algorithm') == 'sha512':
                assert hashlib.sha512(p.read_bytes()).hexdigest() == item['expected_checksum']
    own = R/'output/vm-own-source.tar'
    subprocess.run(['git', '-c', 'safe.directory='+R.as_posix(), 'archive', '--format=tar', '-o', str(own), commit], cwd=R, check=True)
    support = dest/'just-NAS-vm-support-and-sources.tar.gz'
    with tarfile.open(support, 'w:gz') as t:
        for name in ['LICENSE', 'THIRD-PARTY-NOTICES.md', 'VM-SOURCES.md', 'COMPLIANCE.md', 'VM-MANAGER.md', 'VM-VERIFICATION.md', 'README.md']:
            t.add(R/name, arcname='documents/'+name)
        t.add(own, arcname='source/just-NAS-source.tar')
        t.add(source, arcname='source/qemu-runtime')
        t.add(R/'output/vm-compliance/licenses', arcname='licenses/qemu-runtime')
        t.add(R/'output/compliance/iso', arcname='licenses/core-third-party')
        t.add(R/'output/vm-test/runtime-apk-source-coverage.json', arcname='build/runtime-apk-source-coverage.json')
        for name in ['packages.json', 'source-inventory.json']:
            t.add(R/'output/vm-compliance'/name, arcname='build/'+name)
        for name in ['vm-app-build-info.txt', 'vm-core-build-info.txt']:
            t.add(R/'output'/name, arcname='build/'+name)
        # Summary is deliberately public-safe; private per-test artifacts stay local.
        raw = json.dumps({'source_commit': commit, 'result': report['result'],
                          'cold_boot_count': report['cold_boot_count'],
                          'continuous_ready_seconds': report['continuous_ready_seconds'],
                          'kvm': report['kvm'], 'checks': {k: v['result'] for k,v in report['checks'].items()}}, indent=2).encode()
        import io
        info = tarfile.TarInfo('tests/offline-acceptance.json'); info.size = len(raw)
        t.addfile(info, io.BytesIO(raw))
    names.append(support.name)
    (dest/'SHA256SUMS').write_text(''.join(sha(dest/n)+'  '+n+'\n' for n in names), encoding='ascii')
    assert {p.name for p in dest.iterdir()} == set(names+['SHA256SUMS'])
    return commit, names+['SHA256SUMS']

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--directory', type=pathlib.Path, default=R/'output/vm-release')
    parser.add_argument('--publish', action='store_true')
    a = parser.parse_args()
    report = acceptance()
    commit, names = stage(a.directory, report)
    print('PASS five checksummed VM release assets; core ISO excluded', flush=True)
    if not a.publish: return
    branch = git('branch', '--show-current')
    assert branch.startswith('codex/')
    assert git('ls-remote', 'origin', 'refs/heads/'+branch).split()[0] == commit, 'Push tested branch first'
    credential = subprocess.run(['git', '-c', 'safe.directory='+R.as_posix(), 'credential', 'fill'],
                                input='protocol=https\nhost=github.com\n\n', text=True,
                                capture_output=True, check=True, env={**os.environ, 'GIT_TERMINAL_PROMPT':'0'})
    auth = dict(line.split('=',1) for line in credential.stdout.splitlines() if '=' in line)
    s = requests.Session(); s.headers.update({'Authorization':'Bearer '+auth['password'],
        'Accept':'application/vnd.github+json', 'X-GitHub-Api-Version':'2022-11-28'})
    def api(method, path, **kw):
        r = s.request(method, API+path, timeout=60, **kw); r.raise_for_status(); return r.json()
    releases = api('GET', '/releases?per_page=100')
    existing = next((r for r in releases if r['tag_name']=='stores-v2'), None)
    assert existing is None or existing['draft'], 'Refusing to modify a published release'
    body = ('可選配的原生虛擬機管理器與 QEMU 執行環境，不依賴 Docker。'
            '需要本輪相容的本地 rc4 核心候選；此處不發布或替換核心 ISO。\n\n'
            'TCG：標準 Ubuntu Desktop、Windows 10、離線安裝、冷開機與持久化驗證通過。\n'
            + ('KVM：已實測。' if report['kvm']=='PASS' else 'KVM 尚待實際環境驗證；本測試環境無 /dev/kvm。')
            + '\n\n先安裝 qemu-runtime，再安裝 vm-manager；線上商店目錄為 catalog.json。'
            '支援包包含操作、建置及測試說明、專案來源、完整對應 QEMU 執行環境來源與授權。'
            '\n\njust-NAS 自有程式採 MIT；第三方 QEMU、noVNC、韌體與相依套件各自保留原授權，'
            '整個套件並非統一 MIT。既有 stores-v1 與來源分卷保持原樣。')
    rel = existing or api('POST', '/releases', json={'tag_name':'stores-v2', 'target_commitish':commit,
        'name':'just-NAS VM apps — offline-verified preview', 'body':body, 'draft':True, 'prerelease':True})
    uploaded = {v['name']:v for v in api('GET', '/releases/'+str(rel['id'])+'/assets')}
    assert set(uploaded) <= set(names)
    for name in names:
        p = a.directory/name
        if name in uploaded:
            asset = uploaded[name]
        else:
            print('UPLOAD', name, p.stat().st_size, flush=True)
            with p.open('rb') as f:
                r = s.post(rel['upload_url'].split('{',1)[0], params={'name':name}, data=f,
                           headers={'Content-Type':'application/octet-stream'}, timeout=(30,1800))
            r.raise_for_status(); asset = r.json()
        assert asset['size']==p.stat().st_size and asset.get('digest')=='sha256:'+sha(p)
    assets = api('GET', '/releases/'+str(rel['id'])+'/assets')
    assert {v['name'] for v in assets} == set(names)
    rel = api('PATCH', '/releases/'+str(rel['id']), json={'draft':False,'prerelease':True,
        'name':rel['name'], 'body':body, 'make_latest':'false'})
    print('PUBLISHED', rel['html_url'], flush=True)

if __name__ == '__main__': main()
