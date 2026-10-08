#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import sys
import tarfile
import tempfile
import urllib.request

sys.stdout.reconfigure(encoding='utf-8')
sys.stderr.reconfigure(encoding='utf-8')

name=sys.argv[1]
entry=json.loads(Path('ci-tools.lock.json').read_text())[name]
data=urllib.request.urlopen(entry['url'],timeout=120).read()
assert hashlib.sha256(data).hexdigest()==entry['sha256'], '工具文件校验和不匹配'
dest=Path.home()/'.local/bin';dest.mkdir(parents=True,exist_ok=True)
if entry['file'].endswith('.tar.gz'):
    with tempfile.TemporaryFile() as f:
        f.write(data);f.seek(0)
        with tarfile.open(fileobj=f) as t:
            member=next(m for m in t if m.isfile() and Path(m.name).name==name)
            (dest/name).write_bytes(t.extractfile(member).read())
else:(dest/name).write_bytes(data)
(dest/name).chmod(0o755)
if os.getenv('GITHUB_PATH'):
    with open(os.environ['GITHUB_PATH'],'a') as f:f.write(str(dest)+'\n')
