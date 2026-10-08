"""External CI gate: Argo revision, target configuration, deployment and smoke."""
import os,json,time,urllib.request,urllib.error,urllib.parse,base64
from publish_validated import api,REPO
name=os.environ['APP_NAME'];token=os.environ['ARGO_READ_TOKEN'];argo='https://argocd.apps.makima.sbs:9443/api/v1';source=os.environ['GITHUB_SHA']
def get(path):
    req=urllib.request.Request(argo+path,headers={'Authorization':'Bearer '+token})
    with urllib.request.urlopen(req,timeout=60) as r:return json.load(r)
path=f'clusters/production/applications/{name}/values.yaml'
import yaml
v=yaml.safe_load(base64.b64decode(api('GET',f'/repos/{REPO}/contents/{path}')['content']))
assert v['releaseVersion']==source,'应用目标版本在发布后发生变化'
for component in ['foundation','foundation-resources',name]:get('/applications/'+component+'?refresh=hard')
for _ in range(90):
    a=get('/applications/'+name);s=a.get('status',{});sync=s.get('sync',{})
    current=yaml.safe_load(base64.b64decode(api('GET',f'/repos/{REPO}/contents/{path}')['content']))
    if current['releaseVersion']!=source:raise SystemExit('应用已被其他版本替换，无法确认本次发布')
    if sync.get('status')=='Synced' and s.get('health',{}).get('status')=='Healthy':
        complete=True
        for component,w in v['workloads'].items():
            kind=w['kind'];query=urllib.parse.urlencode({'namespace':'app-'+name,'resourceName':name+'-'+component,'version':'v1','kind':kind,'group':'apps' if kind=='Deployment' else 'batch'})
            live=json.loads(get('/applications/'+name+'/resource?'+query)['manifest']);template=live['spec']['template'] if kind=='Deployment' else live['spec']['jobTemplate']['spec']['template'];im=w.get('image') or v['image']
            if template['spec']['containers'][0]['image']!=im['repository']+'@'+im['digest']:complete=False
        if complete:
            host='https://'+v['host']+(':9443' if name=='console' else '')
            try:
                with urllib.request.urlopen(host+'/api',timeout=20) as r:assert r.status<500
            except urllib.error.HTTPError as e:
                if e.code>=500:raise
            print('已确认目标镜像、Argo 健康状态和 HTTPS 冒烟测试，应用：',name);break
    time.sleep(8)
else:raise SystemExit('运行状态验证超时，尚未确认发布成功')
