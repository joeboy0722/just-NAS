"""Collect checksummed corresponding source and notices for the actual release ISO.

Run in Dockerfile.compliance with /work persisted, /input.iso read-only and /scripts.
No private signing keys or development credentials are copied into the collection.
"""
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
import csv, hashlib, io, json, os, re, shutil, subprocess, tarfile, tempfile, time, zipfile
import requests

WORK = Path('/work')
TREE = WORK / 'source-tree'
NOTICES = WORK / 'licenses'
REPO = Path('/aports')
for directory in [TREE, NOTICES]:
    directory.mkdir(parents=True, exist_ok=True)

def run(*args, cwd=None, env=None, timeout=300):
    result = subprocess.run(args, cwd=cwd, env=env, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=timeout)
    if result.returncode:
        raise RuntimeError(f'{args[0]} failed: {result.stderr.decode(errors="replace")[-2000:]}')
    return result.stdout

def log(message):
    print(message, flush=True)

def sha(path, algorithm='sha256'):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, algorithm).hexdigest()

def pkginfo(apk):
    text = run('tar', '-xzOf', str(apk), '.PKGINFO').decode()
    return dict(line.split(' = ', 1) for line in text.splitlines() if ' = ' in line)

def safe_name(value):
    path = Path(value)
    return not path.is_absolute() and '..' not in path.parts

def license_name(name):
    return bool(re.search(r'(^|/)([^/]*(?:licen[cs]e|copying|copyright|notice|whence|unlicense|authors|patents|legal)[^/]*|readme(?:[._-].*)?)$', name, re.I))

def notices(archive, origin):
    dest = NOTICES / origin
    count = 0
    def save(name, data):
        nonlocal count
        if safe_name(name) and len(data) <= 5 * 1024 * 1024:
            target = dest / name
            if target.is_file() and target.read_bytes() != data:
                target = target.with_name(target.name + '.' + hashlib.sha256(data).hexdigest()[:12] + '.txt')
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
            count += 1
    try:
        with tarfile.open(archive, 'r:*') as source:
            for member in source:
                if member.isfile() and member.size <= 5 * 1024 * 1024 and license_name(member.name):
                    save(member.name, source.extractfile(member).read())
    except tarfile.ReadError:
        if zipfile.is_zipfile(archive):
            with zipfile.ZipFile(archive) as source:
                for member in source.infolist():
                    if not member.is_dir() and member.file_size <= 5 * 1024 * 1024 and license_name(member.filename):
                        save(member.filename, source.read(member))
        elif license_name(archive.name):
            save(archive.name, archive.read_bytes())
    return count

def download(urls, target, expected, algorithm='sha512'):
    target.parent.mkdir(parents=True, exist_ok=True)
    if target.is_file() and sha(target, algorithm) == expected:
        return 'verified-cache'
    errors = []
    for url in dict.fromkeys(urls):
        for attempt in range(2):
            try:
                with requests.get(url, stream=True, timeout=(20, 60)) as response:
                    response.raise_for_status()
                    partial = target.with_name(target.name + '.partial')
                    with partial.open('wb') as stream:
                        for chunk in response.iter_content(1024 * 1024):
                            stream.write(chunk)
                if sha(partial, algorithm) != expected:
                    raise ValueError('upstream checksum mismatch')
                partial.replace(target)
                return url
            except (requests.RequestException, OSError, ValueError) as error:
                errors.append(f'{url}: {error}')
                time.sleep(1)
    raise RuntimeError('\n'.join(errors))

with tempfile.TemporaryDirectory() as temp:
    temp = Path(temp)
    run('xorriso', '-osirrox', 'on', '-indev', '/input.iso',
        '-extract', '/apks/x86_64', str(temp / 'apks'),
        '-extract', '/boot/initramfs-lts', str(temp / 'initramfs'),
        '-extract', '/boot/modloop-lts', str(temp / 'modloop'))
    packages = [pkginfo(p) for p in sorted((temp / 'apks').glob('*.apk'))]
    # These packages supply code/firmware to /boot rather than the offline APK repo.
    boot = temp / 'boot-apks'; boot.mkdir()
    run('apk', 'fetch', '--output', str(boot), 'linux-lts', 'zfs-lts',
        'linux-firmware', 'syslinux', 'grub-efi', 'mkinitfs', 'kmod', 'wireless-regdb')
    packages += [dict(pkginfo(p), distribution='boot') for p in sorted(boot.glob('*.apk'))]
    # Record actual boot payload identity; later builds must preserve these bytes.
    boot_identity = {'initramfs_sha256': sha(temp / 'initramfs'), 'modloop_sha256': sha(temp / 'modloop')}
    (WORK / 'boot-identity.json').write_text(json.dumps(boot_identity, indent=2))
    # Initramfs contains a copy of its construction scripts and their provenance.
    initdir = temp / 'initrd'; initdir.mkdir()
    subprocess.run(['sh', '-c', 'gzip -dc "$1" | (cd "$2" && cpio -idmu)', 'sh', str(temp / 'initramfs'), str(initdir)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    shutil.copytree(initdir, TREE / 'initramfs-distributed-files', dirs_exist_ok=True)

(WORK / 'packages.json').write_text(json.dumps(packages, indent=2))
origins = {(p['origin'], p['commit']) for p in packages}
commits = sorted({commit for _, commit in origins})
log(f'Fetching exact aports commits for {len(origins)} source origins')
for offset in range(0, len(commits), 20):
    run('git', '-C', str(REPO), 'fetch', '--depth=1', 'https://github.com/alpinelinux/aports.git', *commits[offset:offset+20], timeout=600)

current = run('git', '-C', str(REPO), 'ls-tree', '-r', '--name-only', 'HEAD').decode().splitlines()
records = []
tasks = []
for origin, commit in sorted(origins):
    matches = [p for p in current if p.endswith('/' + origin + '/APKBUILD')]
    if len(matches) != 1:
        raise RuntimeError(f'Ambiguous recipe: {origin}')
    recipe_path = str(Path(matches[0]).parent)
    dest = TREE / 'recipes' / origin
    dest.mkdir(parents=True, exist_ok=True)
    archive = run('git', '-C', str(REPO), 'archive', commit + ':' + recipe_path)
    with tarfile.open(fileobj=io.BytesIO(archive)) as source:
        source.extractall(dest, filter='data')
    # Use abuild's native architecture/config initialization to evaluate APKBUILD.
    exported = dest / 'APKBUILD.export'
    exported.write_bytes((dest / 'APKBUILD').read_bytes() + b'''\njustnas_export() { printf '%s\\0' "$pkgname" "$pkgver-r$pkgrel" "$source" "$sha512sums" "$sha256sums" "$license"; }\n''')
    fields = run('abuild', '-F', 'justnas_export', cwd=dest,
                 env={**os.environ, 'APKBUILD': str(exported), 'REPODEST': '/tmp/packages'}).decode().split('\0')
    exported.unlink()
    name, version, sources, sums512, sums256, license = fields[:6]
    algorithm = 'sha512' if sums512.strip() else 'sha256'
    sums = dict((filename, value) for value, filename in zip((sums512 or sums256).split()[::2], (sums512 or sums256).split()[1::2]))
    expected_versions = {p['pkgver'] for p in packages if p['origin'] == origin}
    if version not in expected_versions:
        raise RuntimeError(f'Recipe version mismatch {origin}: {version} vs {expected_versions}')
    record = {'origin': origin, 'version': version, 'aports_commit': commit,
              'recipe': recipe_path, 'license': license, 'sources': []}
    for uri in sources.split():
        remote = uri.split('::')[-1]
        filename = uri.split('::')[0].rsplit('/', 1)[-1]
        expected = sums.get(filename)
        if not expected:
            raise RuntimeError(f'Missing source checksum: {origin}/{filename}')
        if remote.startswith(('https://', 'http://', 'ftp://')):
            target = TREE / 'upstream' / origin / filename
            urls = [f'https://distfiles.alpinelinux.org/distfiles/v3.24/{filename}',
                    f'https://distfiles.alpinelinux.org/distfiles/edge/{filename}', remote]
            task = {'origin': origin, 'uri': remote, 'path': str(target.relative_to(TREE)),
                    'checksum_algorithm': algorithm, 'expected_checksum': expected}
            tasks.append((task, urls, target))
        else:
            target = dest / uri
            if not target.is_file() or sha(target, algorithm) != expected:
                raise RuntimeError(f'Local recipe source checksum failed: {origin}/{filename}')
            task = {'origin': origin, 'uri': uri, 'path': str(target.relative_to(TREE)),
                    'checksum_algorithm': algorithm, 'expected_checksum': expected, 'verified': True}
        record['sources'].append(task)
    records.append(record)
    (TREE / 'recipes' / origin / 'APORTS-COMMIT').write_text(commit + '\n')
    for file in dest.rglob('*'):
        if file.is_file() and license_name(file.relative_to(dest).as_posix()):
            target = NOTICES / origin / 'alpine-recipe' / file.relative_to(dest)
            target.parent.mkdir(parents=True, exist_ok=True); shutil.copyfile(file, target)

log(f'Verifying {len(tasks)} upstream source downloads')
def collect(entry):
    record, urls, target = entry
    used = download(urls, target, record['expected_checksum'], record['checksum_algorithm'])
    record.update(verified=True, downloaded_from=used, sha256=sha(target), bytes=target.stat().st_size,
                  notice_files=notices(target, record['origin']))
    log(f'PASS {record["origin"]}/{target.name} ({target.stat().st_size // 1024 // 1024} MiB)')

errors = []
with ThreadPoolExecutor(max_workers=6) as pool:
    futures = {pool.submit(collect, item): item for item in tasks}
    for future in as_completed(futures):
        try: future.result()
        except Exception as error:
            errors.append(str(error)); log('ERROR ' + str(error))
        (WORK / 'source-inventory.json').write_text(json.dumps(records, indent=2))
if errors:
    raise RuntimeError(f'{len(errors)} source fetches failed; see inventory/log; retry uses verified cache')

log('All APK and boot source recipes/downloads verified. Go dependencies and packaging follow separately.')
(WORK / 'sources-collected.ok').write_text('PASS\n')
