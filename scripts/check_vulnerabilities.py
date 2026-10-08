#!/usr/bin/env python3
"""Fail closed for high/critical vulnerabilities; exemptions require reason and expiry."""
from datetime import datetime, timezone
import json
from pathlib import Path
import sys

sys.stdout.reconfigure(encoding='utf-8')
sys.stderr.reconfigure(encoding='utf-8')

report=json.loads(Path(sys.argv[1]).read_text())
assert 'Results' in report and isinstance(report['Results'],list), '扫描报告格式不正确'
waivers=json.loads(Path('security/vulnerability-waivers.json').read_text())
valid=set()
for waiver in waivers:
    assert waiver['reason'].strip(), '漏洞豁免原因不能为空'
    expiry=datetime.fromisoformat(waiver['expiresAt'].replace('Z','+00:00'))
    assert expiry.tzinfo is not None and expiry>datetime.now(timezone.utc), '漏洞豁免已过期或无效'
    valid.add((waiver['id'],waiver['package'],waiver['installedVersion']))
failures=[]
for result in report.get('Results',[]):
    if result.get('Secrets'):
        raise SystemExit('容器镜像中检测到凭据，已阻止发布（凭据值已隐藏）。')
    for v in result.get('Vulnerabilities',[]):
        if v['Severity'] in {'HIGH','CRITICAL'} and (v['VulnerabilityID'],v['PkgName'],v['InstalledVersion']) not in valid:
            failures.append({'漏洞编号':v['VulnerabilityID'],'软件包':v['PkgName'],'严重程度':{'HIGH':'高危','CRITICAL':'严重'}[v['Severity']]})
if failures:
    print(json.dumps(failures,indent=2,ensure_ascii=False))
    raise SystemExit(1)
print('未发现未经豁免的高危或严重漏洞。')
