#!/usr/bin/env python3
"""Verify the expected release and a real translation without logging secrets."""
import json
import os
import sys
import time
import urllib.error
import urllib.request

sys.stdout.reconfigure(encoding='utf-8')
sys.stderr.reconfigure(encoding='utf-8')

version = os.environ['GITHUB_SHA']
base = os.environ.get('RELEASE_URL', 'https://transform.apps.makima.sbs').rstrip('/')
deadline = time.monotonic() + 900
while time.monotonic() < deadline:
    try:
        with urllib.request.urlopen(base + '/api', timeout=15) as response:
            data = json.load(response)
            assert response.status == 200 and data['版本'] == version
        request = urllib.request.Request(
            base + '/api/translate',
            data=json.dumps({'文本': 'Hello, world!'}, ensure_ascii=False).encode(),
            headers={'Content-Type': 'application/json'},
        )
        with urllib.request.urlopen(request, timeout=75) as response:
            data = json.load(response)
            assert response.status == 200 and response.headers['x-release-version'] == version
            assert data['译文'].strip() and data['目标语言'] == '简体中文'
            assert data['模型'] == 'north-small-translate-1-0' and data['完成状态'] == '已完成'
        print('目标版本已通过可信 HTTPS 响应，并完成一次真实翻译。')
        break
    except (urllib.error.URLError, TimeoutError, AssertionError, KeyError, json.JSONDecodeError):
        time.sleep(15)
else:
    raise SystemExit('发布验证超时，请检查 Argo 和工作负载事件。')
