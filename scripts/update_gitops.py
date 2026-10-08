import os,base64,io
from ruamel.yaml import YAML
from publish_validated import api,publish,REPO
name=os.environ.get('APP_NAME','transform');path=f'clusters/production/applications/{name}/values.yaml'
base=api('GET',f'/repos/{REPO}/git/ref/heads/main')['object']['sha'];body=api('GET',f'/repos/{REPO}/contents/{path}?ref={base}')
y=YAML();y.preserve_quotes=True;v=y.load(base64.b64decode(body['content']).decode());v.setdefault('image',{}).update(repository=os.environ['IMAGE'],digest=os.environ.get('DIGEST') or os.environ['IMAGE_DIGEST'],pullPolicy='IfNotPresent',pullSecret='ghcr-pull');v['releaseVersion']=os.environ['GITHUB_SHA'];out=io.StringIO();y.dump(v,out)
publish({path:out.getvalue()},'Release '+name+' '+v['releaseVersion'][:12],base)
