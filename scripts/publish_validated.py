"""Shared, conflict-aware, validated fast-forward publication. Never force main."""
import os,json,time,urllib.request,urllib.error,base64,uuid,io,sys
from ruamel.yaml import YAML
sys.stdout.reconfigure(encoding='utf-8')
sys.stderr.reconfigure(encoding='utf-8')
TOKEN=os.environ['FOUNDATION_TOKEN'];REPO=os.environ.get('FOUNDATION_REPO','Yinlerens/cloud-foundation')
def api(method,path,data=None,missing=False):
    req=urllib.request.Request('https://api.github.com'+path,method=method,headers={'Authorization':'Bearer '+TOKEN,'Accept':'application/vnd.github+json','Content-Type':'application/json'},data=json.dumps(data).encode() if data is not None else None)
    try:
        with urllib.request.urlopen(req,timeout=45) as r:return json.load(r) if r.status!=204 else {}
    except urllib.error.HTTPError as e:
        if missing and e.code==404:return None
        raise RuntimeError('GitHub 操作被阻止，HTTP 状态码：'+str(e.code)) from None

def read(path,ref):
    body=api('GET',f'/repos/{REPO}/contents/{path}?ref={ref}',missing=True)
    return base64.b64decode(body['content']).decode() if body else None

def delta(before,after):
    out={}
    for k in set(before)|set(after):
        if before.get(k)==after.get(k):continue
        if k not in after:out[k]=None
        elif isinstance(before.get(k),dict) and isinstance(after[k],dict):out[k]=delta(before[k],after[k])
        else:out[k]=after[k]
    return out

def merge(base,current,patch,path=''):
    for key,value in patch.items():
        name=path+str(key)
        if isinstance(value,dict) and isinstance(base.get(key),dict) and isinstance(current.get(key),dict):merge(base[key],current[key],value,name+'.')
        else:
            if base.get(key)!=current.get(key):raise RuntimeError('并发修改导致字段冲突：'+name)
            if value is None:current.pop(key,None)
            else:current[key]=value
    return current

def rebase(files,original,newbase):
    result={}
    for path,proposed in files.items():
        base=read(path,original);current=read(path,newbase)
        if base==current:result[path]=proposed;continue
        if proposed is not None and base is not None and current is not None and path.endswith(('.yaml','.yml')):
            y=YAML();y.preserve_quotes=True;b=y.load(base);c=y.load(current);p=y.load(proposed)
            if not all(isinstance(x,dict) for x in [b,c,p]):raise RuntimeError('并发修改导致文件冲突：'+path)
            merged=merge(b,c,delta(b,p));output=io.StringIO();y.dump(merged,output);result[path]=output.getvalue()
        else:raise RuntimeError('并发修改导致文件冲突：'+path)
    return result

def publish(files,message,base=None):
    original=base or api('GET',f'/repos/{REPO}/git/ref/heads/main')['object']['sha']
    for attempt in range(3):
        head=api('GET',f'/repos/{REPO}/git/ref/heads/main')['object']['sha'];proposal=files if head==original else rebase(files,original,head)
        commit=api('GET',f'/repos/{REPO}/git/commits/{head}');entries=[]
        for path,text in proposal.items():
            if text is None:
                if read(path,head) is not None:entries.append({'path':path,'mode':'100644','type':'blob','sha':None})
            else:
                sha=api('POST',f'/repos/{REPO}/git/blobs',{'encoding':'utf-8','content':text})['sha'];entries.append({'path':path,'mode':'100644','type':'blob','sha':sha})
        tree=api('POST',f'/repos/{REPO}/git/trees',{'base_tree':commit['tree']['sha'],'tree':entries})['sha'];sha=api('POST',f'/repos/{REPO}/git/commits',{'message':message,'parents':[head],'tree':tree})['sha'];branch='delivery/'+str(uuid.uuid4())
        api('POST',f'/repos/{REPO}/git/refs',{'ref':'refs/heads/'+branch,'sha':sha});api('POST',f'/repos/{REPO}/actions/workflows/configuration.yml/dispatches',{'ref':branch})
        for _ in range(150):
            runs=api('GET',f'/repos/{REPO}/actions/workflows/configuration.yml/runs?head_sha={sha}')['workflow_runs']
            if runs and runs[0]['status']=='completed':
                if runs[0]['conclusion']!='success':raise RuntimeError('配置验证失败：'+runs[0]['html_url'])
                latest=api('GET',f'/repos/{REPO}/git/ref/heads/main')['object']['sha']
                if latest!=head:break
                try:api('PATCH',f'/repos/{REPO}/git/refs/heads/main',{'sha':sha,'force':False})
                except RuntimeError:
                    if api('GET',f'/repos/{REPO}/git/ref/heads/main')['object']['sha']!=head:break
                    raise
                print('已发布通过验证的版本：',sha);return sha
            time.sleep(8)
        else:raise RuntimeError('验证超时，main 分支未被修改')
        print('检测到并发修改，正在合并无冲突的字段并重新验证')
    raise RuntimeError('多次出现并发发布，未执行覆盖操作')
