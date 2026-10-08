#!/usr/bin/env python3
"""Update the registered translate service and optionally rotate its model key."""
import base64
import copy
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import uuid

import yaml

SOURCE = 'Yinlerens/translate'
APP = 'transform'
NAMESPACE = 'app-transform'
VALUES = 'clusters/production/applications/transform/values.yaml'
SECRET = 'clusters/production/applications/transform/secrets/transform-api.yaml'
CERTIFICATE = Path('.foundation/clusters/production/sealed-secrets.crt')


def seal_model_key(value):
    obj = {'apiVersion': 'v1', 'kind': 'Secret', 'type': 'Opaque',
           'metadata': {'name': 'transform-api', 'namespace': NAMESPACE},
           'data': {'cohere-api-key': base64.b64encode(value.encode()).decode()}}
    result = subprocess.run(
        ['kubeseal', '--cert', str(CERTIFICATE), '--scope=strict', '--format=json'],
        input=json.dumps(obj), text=True, capture_output=True, timeout=30)
    if result.returncode:
        raise RuntimeError('模型密钥加密失败，未提交配置。')
    doc = json.loads(result.stdout)
    text = yaml.safe_dump(doc, sort_keys=False)
    for ciphertext in doc['spec']['encryptedData'].values():
        text = text.replace(ciphertext, ciphertext + ' # gitleaks:allow -- Sealed Secrets ciphertext')
    return text


def release_proposal(current, image, model_key=''):
    if not current:
        raise RuntimeError('transform 尚未在平台登记，此更新流程不会创建新应用。')
    if current.get('app') != APP:
        raise RuntimeError('现有应用名称与 transform 不一致，停止更新。')
    if image.get('repository') != 'ghcr.io/yinlerens/translate' or not re.fullmatch(
            r'sha256:[a-f0-9]{64}', image.get('digest', '')) or not re.fullmatch(
            r'[a-f0-9]{40}', image.get('source_sha', '')):
        raise RuntimeError('镜像与源码仓库不一致或版本无效，停止更新。')
    v = copy.deepcopy(current)
    v.setdefault('image', {}).update(repository=image['repository'], digest=image['digest'],
                                    pullPolicy='IfNotPresent', pullSecret='ghcr-pull')
    v['releaseVersion'] = image['source_sha']
    files = {}
    model_key = model_key.strip()
    if model_key:
        workload = v.get('workloads', {}).get('api')
        expected = {'name': 'transform-api', 'key': 'cohere-api-key'}
        if not workload or workload.get('secretEnv', {}).get('COHERE_API_KEY') != expected:
            raise RuntimeError('应用未引用预期的模型 Secret，停止密钥轮换。')
        files[SECRET] = seal_model_key(model_key)
        workload.setdefault('annotations', {})['foundation.makima.sbs/rollout-revision'] = str(uuid.uuid4())
    files[VALUES] = yaml.safe_dump(v, sort_keys=False)
    return files


def main():
    if sys.argv[1:]:
        raise RuntimeError('不支持的更新参数。')
    if os.environ.get('GITHUB_REPOSITORY', '').lower() != SOURCE.lower():
        raise RuntimeError('此更新工具只适用于 Yinlerens/translate。')
    from publish_validated import api, read, publish, REPO
    base = api('GET', f'/repos/{REPO}/git/ref/heads/main')['object']['sha']
    current = read(VALUES, base)
    image = {'repository': os.environ['IMAGE'], 'digest': os.environ['IMAGE_DIGEST'],
             'source_sha': os.environ['GITHUB_SHA']}
    files = release_proposal(yaml.safe_load(current) if current else None, image,
                             os.environ.get('COHERE_API_KEY', ''))
    publish(files, 'Release translate ' + image['source_sha'][:12], base)


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, ValueError, KeyError) as error:
        print('发布未完成：' + str(error), file=sys.stderr)
        raise SystemExit(1)
