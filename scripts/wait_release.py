#!/usr/bin/env python3
"""Observe the English canary through Waypoint, then run one paid business smoke."""
import json
import os
import sys
import time
import urllib.error
import urllib.request

sys.stdout.reconfigure(encoding='utf-8')
version = os.environ['GITHUB_SHA']
base = os.environ.get('RELEASE_URL', 'https://transform.apps.makima.sbs').rstrip('/')
deadline = time.monotonic() + 900
metadata_seen = False
error_seen = False
while time.monotonic() < deadline:
    try:
        with urllib.request.urlopen(base + '/api', timeout=15) as response:
            data = json.load(response)
            if response.headers.get('X-Release-Version') == version:
                assert response.headers.get('X-API-Contract') == 'english-v2'
                assert data['version'] == version and '版本' not in data
                metadata_seen = True
        # Invalid input never calls the model, including on the stable version.
        request = urllib.request.Request(base + '/api/translate', data=b'{}',
            headers={'Content-Type': 'application/json'})
        try:
            urllib.request.urlopen(request, timeout=15).close()
            raise AssertionError('Invalid input unexpectedly accepted')
        except urllib.error.HTTPError as response:
            data = json.load(response)
            if response.headers.get('X-Release-Version') == version:
                assert response.code == 400 and 'error' in data and '错误' not in data
                assert set(data['error']) == {'code', 'message', 'request_id'}
                error_seen = True
        if metadata_seen and error_seen:
            break
    except (urllib.error.URLError, TimeoutError, AssertionError, KeyError, json.JSONDecodeError):
        pass
    time.sleep(1)
else:
    raise SystemExit('未能从公网观察到目标英文金丝雀；检查 Waypoint 权重和实例状态。')

# Exactly one real translation; weighted routing may send it to either version.
request = urllib.request.Request(base + '/api/translate',
    data=json.dumps({'text': 'Hello, world!'}).encode(),
    headers={'Content-Type': 'application/json'})
with urllib.request.urlopen(request, timeout=75) as response:
    data = json.load(response)
    if response.headers.get('X-API-Contract') == 'english-v2':
        assert data['translated_text'].strip() and data['target_language'] == '简体中文'
        assert data['model'] == 'north-small-translate-09-2026' and data['finish_reason'] == '已完成'
    else:
        assert data['译文'].strip() and data['目标语言'] == '简体中文'
        assert data['模型'] == 'north-small-translate-09-2026' and data['完成状态'] == '已完成'
print('已从公网观察到目标英文金丝雀和英文错误响应，并完成一次真实翻译。')
