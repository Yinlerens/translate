#!/usr/bin/env python3
"""Update only the registered remote canary; never promote the stable release."""
import copy
import os
import re
import sys

import yaml

SOURCE = 'Yinlerens/translate'
APP = 'transform'
VALUES = 'clusters/production/applications/transform/values.yaml'


def release_proposal(current, image, model_key=''):
    if not current:
        raise RuntimeError('transform 尚未在平台登记，此更新流程不会创建新应用。')
    if current.get('app') != APP:
        raise RuntimeError('现有应用名称与 transform 不一致，停止更新。')
    if image.get('repository') != 'ghcr.io/yinlerens/translate' or not re.fullmatch(
            r'sha256:[a-f0-9]{64}', image.get('digest', '')) or not re.fullmatch(
            r'[a-f0-9]{40}', image.get('source_sha', '')):
        raise RuntimeError('镜像与源码仓库不一致或版本无效，停止更新。')
    canary = current.get('mesh', {}).get('canary', {})
    if not current.get('mesh', {}).get('enabled') or not canary.get('enabled') or (
            canary.get('stableWorkload'), canary.get('candidateWorkload')) != ('api', 'api-canary'):
        raise RuntimeError('平台尚未配置预期的 Waypoint 金丝雀，禁止更新稳定版本。')
    stable = current.get('workloads', {}).get('api', {})
    candidate = current.get('workloads', {}).get('api-canary', {})
    if not stable.get('image', {}).get('digest') or not stable.get('releaseVersion') or (
            candidate.get('nodeSelector', {}).get('foundation.makima.sbs/site') != 'remote'):
        raise RuntimeError('稳定版本未独立固定或金丝雀未绑定远端节点，停止更新。')
    if model_key.strip():
        raise RuntimeError('金丝雀更新不轮换共享模型密钥；请通过独立的密钥轮换流程操作。')
    v = copy.deepcopy(current)
    workload = v['workloads']['api-canary']
    workload['image'] = dict(current['image'], repository=image['repository'], digest=image['digest'])
    workload['releaseVersion'] = image['source_sha']
    return {VALUES: yaml.safe_dump(v, sort_keys=False, allow_unicode=True)}


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
    files = release_proposal(yaml.safe_load(current) if current else None, image)
    publish(files, 'Release translate canary ' + image['source_sha'][:12], base)


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, ValueError, KeyError) as error:
        print('发布未完成：' + str(error), file=sys.stderr)
        raise SystemExit(1)
