#!/usr/bin/env python3
"""Real startup auto-setup with fixed HTTPS provider/DNS fixtures, no public network."""
import ipaddress, json, os, pathlib, subprocess, sys, time, uuid
image=os.environ.get('CONTAINER_TEST_IMAGE','caddy-admin:single-container-test')
prefix='caddy-admin-auto-'+uuid.uuid4().hex[:8]
network=prefix+'-net';containers=[];volumes=[]
provider=sys.argv[1] if len(sys.argv)>1 else os.environ.get('TEST_DNS_PROVIDER','cloudflare')
if provider not in ('cloudflare','dnspod'):raise ValueError('provider must be cloudflare or dnspod')
password='IsolatedAutoSetupOnly$123!';token=('12345,'+'a'*32) if provider=='dnspod' else 'a'*40
token_file='dnspod_token' if provider=='dnspod' else 'cloudflare_token'
other_provider='cloudflare' if provider=='dnspod' else 'dnspod'

def docker(*args,check=True):
    result=subprocess.run(['docker',*args],capture_output=True,text=True,timeout=90)
    if check and result.returncode:raise RuntimeError('Docker fixture command failed: '+result.stderr)
    return result

def eventually(fn):
    deadline=time.monotonic()+60
    while time.monotonic()<deadline:
        try:
            value=fn()
            if value:return value
        except (ValueError,RuntimeError):pass
        time.sleep(.25)
    raise RuntimeError('isolated automatic setup did not become ready')

def mounts(name):
    result=[]
    for suffix,target in [('manager','/var/lib/manager'),('snapshots','/srv/snapshots'),('data','/data'),('config','/config')]:
        volume=prefix+'-'+name+'-'+suffix;docker('volume','create',volume);volumes.append(volume);result+=['-v',volume+':'+target]
    return result

def request(container,path,body=None,formal=False,session=False,csrf=None):
    host='caddyadmin.home.example.test' if formal else '127.0.0.1'
    url='https://'+host+path
    args=['exec',container,'curl','-H','Origin: https://'+host,'--noproxy','*','-sk','--max-time','5','-w','\n%{http_code}','--resolve',host+':443:127.0.0.1',url]
    if session:args+=['-b','/tmp/setup-test.cookies','-c','/tmp/setup-test.cookies']
    if csrf:args+=['-H','X-CSRF-Token: '+csrf]
    if body is not None:args+=['-H','Origin: https://'+host,'-H','Content-Type: application/json','--data',json.dumps(body)]
    response=docker(*args).stdout;payload,status=response.rsplit('\n',1)
    return int(status),json.loads(payload) if payload else None

try:
    docker('network','create','--internal',network)
    subnet=docker('network','inspect','-f','{{(index .IPAM.Config 0).Subnet}}',network).stdout.strip()
    scope=ipaddress.ip_network(subnet);entry=str(scope.network_address+10);pause_entry=str(scope.network_address+11)
    cert_volume=prefix+'-fixture';docker('volume','create',cert_volume);volumes.append(cert_volume)
    fixture=prefix+'-fixture';containers.append(fixture)
    binary=pathlib.Path('bin/setup-fixture').resolve()
    docker('run','-d','--name',fixture,'--network',network,'-v',str(binary)+':/setup-fixture:ro','-v',cert_volume+':/fixture','-e','FIXTURE_ADDRESS='+entry,'--entrypoint','/setup-fixture',image)
    fixture_ip=docker('inspect','-f','{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}',fixture).stdout.strip()
    eventually(lambda:docker('exec',fixture,'test','-f','/fixture/ca.crt',check=False).returncode==0)
    storage=mounts('instance')
    def start(name,address,data,changed=False):
        container=prefix+'-'+name;containers.append(container)
        values={'ADMIN_PASSWORD':'bad' if changed else password,'DNS_PROVIDER':other_provider if changed else provider,'CLOUDFLARE_API_TOKEN':'bad' if changed else (token if provider=='cloudflare' else ''),'DNSPOD_TOKEN':'bad' if changed else (token if provider=='dnspod' else ''),'SETUP_DOMAIN':'bad/domain' if changed else '*.home.example.test','SETUP_CADDY_IP':'::' if changed else address,'SETUP_ADMIN_USERNAME':'operator','SETUP_RESOLVERS':fixture_ip+':53','SSL_CERT_FILE':'/fixture/ca.crt','HTTP_PROXY':'','HTTPS_PROXY':'','ALL_PROXY':'','NO_PROXY':'*'}
        env=[arg for key,value in values.items() for arg in ('-e',key+'='+value)]
        docker('run','-d','--init','--restart','unless-stopped','--name',container,'--network',network,'--ip',address,'--add-host','api.cloudflare.com:'+fixture_ip,'--add-host','cloudflare-dns.com:'+fixture_ip,'--add-host','dnsapi.cn:'+fixture_ip,'-v',cert_volume+':/fixture:ro',*data,*env,image)
        return container
    instance=start('instance',entry,storage)
    eventually(lambda:docker('exec',instance,'manager','health',check=False).returncode==0 and request(instance,'/api/v1/setup/handoff')[1].get('initialized'))
    status,login=request(instance,'/api/v1/auth/login',{'username':'operator','password':password},True,session=True);assert status==200,'automatic administrator missing'
    status,_=request(instance,'/api/v1/services',formal=True);assert status==401,'unauthenticated API accepted'
    status,settings=request(instance,'/api/v1/settings',formal=True,session=True);assert status==200,'authenticated settings unavailable'
    assert settings['dns_provider']==provider and settings['certificate_status']['mode']==provider and settings['token_configured'],'selected provider not saved'
    policy=settings['config']
    assert policy['domains'][0]['access']=='internet' and not policy['console_lan_only'],'default source policy was restricted'
    assert policy['lan_cidrs']==['0.0.0.0/0','::/0'] and policy['upstream_cidrs']==['0.0.0.0/0','::/0'] and policy['allowed_names']==['*'] and policy['denied_ips']==[],'default target policy was restricted'
    status,draft=request(instance,'/api/v1/services',formal=True,session=True);assert status==200 and draft['services']==[] and draft['published']==[],'initial service list was not empty'
    sample={'name':'Isolated named upstream','domain_id':policy['domains'][0]['id'],'hostname':'sample.home.example.test','scheme':'https','host':fixture,'port':443,'enabled':True,'notes':'Test fixture only'}
    status,_=request(instance,'/api/v1/services',{'revision':draft['revision'],'service':sample},True,session=True);assert status==403,'service mutation bypassed CSRF'
    status,_=request(instance,'/api/v1/services',{'revision':draft['revision'],'service':sample},True,session=True,csrf=login['csrf']);assert status==200,'open policy rejected named fixture upstream'
    status,public=request(instance,'/api/v1/setup/handoff');assert status==200 and public['initialized'] and public['console_status']!='ready','test certificate reported public trust'
    snapshot=docker('exec',instance,'cat','/srv/snapshots/active.json').stdout
    assert token not in snapshot and password not in snapshot,'secret leaked into startup snapshot'
    assert any(issuer.get('challenges',{}).get('dns',{}).get('provider',{}).get('name')==provider for issuer in json.loads(snapshot)['apps']['tls']['automation']['policies'][0]['issuers']),'wrong runtime DNS module'
    assert sample['hostname'] not in snapshot,'saving the first service draft published a business route'
    saved_token=docker('exec',instance,'cat','/var/lib/manager/secrets/'+token_file).stdout
    assert saved_token.strip()==token,'token not persisted'
    docker('restart','-t','20',instance)
    eventually(lambda:docker('exec',instance,'manager','health',check=False).returncode==0)
    assert request(instance,'/api/v1/auth/login',{'username':'operator','password':password},True)[0]==200,'restart lost administrator'
    docker('rm','-f',instance)
    changed=start('changed',entry,storage,True)
    eventually(lambda:docker('exec',changed,'manager','health',check=False).returncode==0)
    assert request(changed,'/api/v1/auth/login',{'username':'operator','password':password},True,session=True)[0]==200,'changed environment reset password or prevented startup'
    assert docker('exec',changed,'cat','/srv/snapshots/active.json').stdout==snapshot,'changed environment overwrote snapshot'
    assert docker('exec',changed,'cat','/var/lib/manager/secrets/'+token_file).stdout==saved_token,'changed environment overwrote token'
    code,restarted_settings=request(changed,'/api/v1/settings',formal=True,session=True)
    assert code==200 and restarted_settings['dns_provider']==provider and restarted_settings['certificate_status']['mode']==provider and restarted_settings['token_configured'],'changed environment overrode saved provider or credential'
    paused=start('paused',pause_entry,mounts('paused'))
    progress=eventually(lambda: (lambda result:result[1] if result[0]==200 and result[1]['status']=='action_required' else None)(request(paused,'/api/v1/setup/progress')))
    assert progress['stage']=='dns','wrong pause phase'
    for _ in range(25):assert request(paused,'/api/v1/setup/progress')[0]==200,'polling exhausted rate limit'
    assert docker('exec',paused,'test','-f','/srv/snapshots/active.json',check=False).returncode!=0,'paused task wrote snapshot'
    nonce=request(paused,'/api/v1/setup/status')[1]['setup_id']
    for _ in range(2):
        code,result=request(paused,'/api/v1/setup/recheck',{'setup_id':nonce})
        assert code==200 and result['status']=='action_required','readonly recheck failed'
    assert docker('exec',paused,'test','-f','/srv/snapshots/active.json',check=False).returncode!=0,'recheck saved initialization'
    stats=json.loads(docker('exec',fixture,'curl','--noproxy','*','-sk','https://127.0.0.1/stats').stdout)
    assert stats['reads']>0 and stats['writes']==0,'automatic flow wrote provider DNS'
    assert (stats['dnspod_reads']>0)==(provider=='dnspod') and stats['dnspod_writes']==0,'wrong DNS provider API used'
    for name in (changed,paused):
        logs=docker('logs',name).stdout+docker('logs',name).stderr
        assert token not in logs and password not in logs,'credential leaked to logs'
    print('PASS automatic startup ('+provider+'): real Manager/Caddy, isolated HTTPS provider/DNS, read-only reuse, sole administrator, authenticated API, temporary certificate uncertainty, restart persistence, changed environment ignored, DNS change paused without writes',flush=True)
except Exception:
    for container in containers:
        diagnostic=docker('logs','--tail','20',container,check=False)
        print((diagnostic.stdout+diagnostic.stderr).replace(password,'[redacted]').replace(token,'[redacted]'))
    raise
finally:
    for container in reversed(containers):docker('rm','-f',container,check=False)
    for volume in volumes:docker('volume','rm',volume,check=False)
    docker('network','rm',network,check=False)
