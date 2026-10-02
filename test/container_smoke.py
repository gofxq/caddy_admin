#!/usr/bin/env python3
"""Exercise Compose deployment, first-run and normal operation in isolated Docker resources."""
import concurrent.futures, json, os, pathlib, subprocess, tempfile, time, uuid

image = os.environ.get('CONTAINER_TEST_IMAGE', 'caddy-admin:single-container-test')
prefix = 'caddy-admin-smoke-' + uuid.uuid4().hex[:8]
containers, volumes = [], []
network = prefix + '-net'
compose_project = prefix + '-compose'
work = tempfile.TemporaryDirectory(prefix=prefix)
root = pathlib.Path(work.name)
password = 'ContainerSmokeOnly123!'
probe = ''

def run(*args, check=True):
    p = subprocess.run(args, capture_output=True, text=True, timeout=60)
    if check and p.returncode:
        raise RuntimeError(str(args) + '\n' + p.stdout + p.stderr)
    return p

def docker(*args, **kw): return run('docker', *args, **kw)
def compose(*args, **kw): return docker('compose','-p',compose_project,'-f','compose.yaml','-f','compose.build.yaml','-f','compose.validation.yaml',*args,**kw)
def curl(*args, **kw): return run('docker','exec',probe,'curl',*args,**kw)

def eventually(fn, timeout=45):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        try:
            if fn(): return
        except Exception: pass
        time.sleep(.25)
    raise RuntimeError('readiness timed out')

def env(values): return [x for k,v in values.items() for x in ('-e', k+'='+v)]

def data(name):
    mounts=[]
    for suffix,target in [('db','/var/lib/manager'),('snap','/srv/snapshots'),('data','/data'),('config','/config')]:
        volume=prefix+'-'+name+'-'+suffix;docker('volume','create',volume);volumes.append(volume);mounts += ['-v',volume+':'+target]
    return mounts

def start(name,mounts,values,alias=None,command=()):
    name=prefix+'-'+name;containers.append(name)
    args=['run','-d','--init','--restart','unless-stopped','--name',name,'--network',network]
    if alias:args += ['--network-alias',alias]
    args += mounts+env(values)+[image]+list(command);docker(*args);return name

def setup(container,values,services=None):
    address=docker('inspect','-f','{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}',container).stdout.strip()
    origin='https://'+address+(':8082' if values.get('CADDY_ADMIN_URL') else '')
    if not values.get('CADDY_ADMIN_URL'):
        redirect=curl('--noproxy','*','-s','--connect-timeout','2','--max-time','8','-D','-', '-o','/dev/null','http://'+address+'/')
        assert '302' in redirect.stdout and 'https://'+address+'/setup' in redirect.stdout,'HTTP did not redirect to HTTPS Setup'
        insecure_post=curl('--noproxy','*','-s','--connect-timeout','2','--max-time','8','-w','\n%{http_code}','-X','POST','http://'+address+'/api/v1/setup/complete')
        assert insecure_post.stdout.rsplit('\n',1)[1]=='405','Setup accepted a plaintext POST'
    assert docker('exec',container,'test','-e','/var/lib/manager/secrets/setup_token',check=False).returncode!=0,'obsolete setup token was created'
    def request(path,body=None):
        args=['--noproxy','*','-sk','--connect-timeout','2','--max-time','8','-w','\n%{http_code}',origin+path]
        if body is not None:args += ['-H','Origin: '+origin,'-H','Content-Type: application/json','--data',json.dumps(body)]
        try:
            output=curl(*args).stdout
        except Exception as error:
            logs=docker('logs',container,check=False)
            state=docker('inspect','-f','{{.State.Status}} {{.State.ExitCode}}',container,check=False)
            raise AssertionError('request %s failed: %s\\nstate=%s\\n%s\\n%s' % (path,error,state.stdout,logs.stdout,logs.stderr))
        payload,status=output.rsplit('\n',1);return int(status),payload
    try:
        status,_=request('/api/v1/auth/login')
    except Exception as error:
        logs=docker('logs',container,check=False)
        inside=docker('exec',container,'wget','--no-check-certificate','-qSO-','https://127.0.0.1'+(':8082' if values.get('CADDY_ADMIN_URL') else '')+'/api/v1/setup/status',check=False)
        raise AssertionError('setup endpoint did not accept a connection: %s\\ninside=%s\\n%s\\n%s' % (error,inside.stdout+inside.stderr,logs.stdout,logs.stderr))
    assert status==404, 'normal authentication API was available before setup'
    def assert_no_redundant_setup_listener():
        if not values.get('CADDY_ADMIN_URL'):
            response=curl('--noproxy','*','-sk','--connect-timeout','2','--max-time','8','https://'+address+':8082/',check=False)
            assert response.returncode==7,'internal mode retained an independent Setup listener'
    assert_no_redundant_setup_listener()
    status,_=request('/api/v1/setup/status')
    if status!=200:
        logs=docker('logs',container,check=False)
        raise AssertionError('anonymous setup status failed: HTTP %s %s\\n%s\\n%s' % (status,_,logs.stdout,logs.stderr))
    hostname='caddyadmin.home.example.test'
    body={'username':'admin','password':password,
          'settings':{'homelab_domain':'home.example.test',
          'lan_cidrs':['127.0.0.0/8','10.0.0.0/8','172.16.0.0/12','192.168.0.0/16'],'upstream_cidrs':['172.16.0.0/12'],
          'allowed_names':[],'denied_ips':[],'resolvers':['9.9.9.9']}}
    if services is not None:body['services']=services
    preflight_status,preflight_payload=request('/api/v1/setup/preflight',{'settings':body['settings'],**({'services':services} if services is not None else {})})
    assert preflight_status==200 and 'requires_acknowledgement' in preflight_payload,(preflight_status,preflight_payload)
    unacknowledged,_=request('/api/v1/setup/complete',body)
    assert unacknowledged==409,'setup warnings were accepted without explicit acknowledgement'
    body['acknowledge_warnings']=True
    if services:
        unconfirmed,_=request('/api/v1/setup/complete',body)
        assert unconfirmed==422,'imported setup accepted without explicit confirmation'
        body['confirm_import']=True
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        results=list(pool.map(lambda _:request('/api/v1/setup/complete',body),range(2)))
    assert sorted(status for status,_ in results)==[202,409],results
    eventually(lambda:docker('exec',container,'manager','health',check=False).returncode==0)
    if services and not values.get('CADDY_ADMIN_URL'):
        snapshot=docker('exec',container,'cat','/srv/snapshots/active.json').stdout
        assert services[0]['hostname'] not in snapshot,'setup published imported business routes'
    def setup_api_closed():
        response=curl('--noproxy','*','-sk','--connect-timeout','2','--max-time','8','-w','\\n%{http_code}',origin+'/api/v1/setup/status',check=False)
        if response.returncode!=0:return False
        return int(response.stdout.rsplit('\n',1)[1])!=200
    eventually(setup_api_closed)
    assert_no_redundant_setup_listener()
    after=curl('--noproxy','*','-sk','--connect-timeout','2','--max-time','8','-w','\\n%{http_code}',
               origin+'/api/v1/setup/status',check=False)
    assert after.returncode==0,'temporary handoff entry was unavailable after initialization'
    _,after_status=after.stdout.rsplit('\n',1)
    assert int(after_status)==404,'anonymous setup API remained available after initialization'
    handoff=curl('--noproxy','*','-sk','--connect-timeout','2','--max-time','8','-w','\n%{http_code}',origin+'/api/v1/setup/handoff',check=False)
    _,handoff_status=handoff.stdout.rsplit('\n',1)
    assert int(handoff_status)==200,'handoff API was unavailable after initialization'
    if not values.get('CADDY_ADMIN_URL'):
        forged=curl('--noproxy','*','-sk','--connect-timeout','2','--max-time','8','-w','\n%{http_code}','-H','Host: unknown.example.test',origin+'/api/v1/setup/handoff')
        assert forged.stdout.rsplit('\n',1)[1]=='404','an unregistered domain reached the IP handoff'
    if values.get('CADDY_ADMIN_URL'):
        external_login=curl('--noproxy','*','-sk','--connect-timeout','2','--max-time','8','-w','\n%{http_code}',origin+'/api/v1/auth/login',check=False)
        _,external_login_status=external_login.stdout.rsplit('\n',1)
        assert int(external_login_status)==404,'external handoff entry exposed the management login API'
    else:
        rescue_login=curl('--noproxy','*','-sk','--connect-timeout','2','--max-time','8','-w','\\n%{http_code}',
            origin+'/api/v1/auth/login','-H','Origin: '+origin,'-H','Content-Type: application/json',
            '--data',json.dumps({'username':'admin','password':password}),check=False)
        assert rescue_login.returncode==0,'rescue login endpoint was unavailable'
        _,login_status=rescue_login.stdout.rsplit('\n',1)
        assert int(login_status)==200,'rescue entry did not require and accept administrator authentication'
    processes=docker('top',container,'-eo','pid,args').stdout
    return origin,hostname,processes

def api_client(container,hostname,target):
    cookies='/tmp/'+container+'.cookies'
    def request(path,body=None,csrf=None):
        args=['--noproxy','*','-sk','--connect-timeout','2','--max-time','8','--connect-to',hostname+':443:'+target+':443',
              '-b',cookies,'-c',cookies,'-w','\n%{http_code}','https://'+hostname+path]
        if body is not None:args += ['-H','Content-Type: application/json','-H','Origin: https://'+hostname,'--data',json.dumps(body)]
        if csrf:args += ['-H','X-CSRF-Token: '+csrf]
        out=curl(*args).stdout;payload,status=out.rsplit('\n',1);return int(status),payload
    return request

def exercise(request,bootstrap=False):
    eventually(lambda:request('/')[0]==200)
    status,page=request('/services');assert status==200 and '<html' in page,(status,page)
    status,body=request('/api/v1/auth/login',{'username':'admin','password':password});assert status==200,(status,body)
    csrf=json.loads(body)['csrf']
    status,body=request('/api/v1/configuration/export');assert status==200,(status,body)
    configuration=json.loads(body)
    assert configuration['format']=='caddy-web-admin' and configuration['version']==1
    assert all(set(service)=={'name','group','hostname','scheme','host','port','enabled','notes'} for service in configuration['services'])
    before=json.loads(request('/api/v1/services')[1])
    runtime=json.loads(request('/api/v1/draft/preview')[1])['runtime_hash']
    empty={**configuration,'services':[]}
    status,body=request('/api/v1/configuration/preview',{'configuration':empty},csrf);assert status==200,(status,body)
    revision=json.loads(body)['revision']
    status,_=request('/api/v1/configuration/import',{'configuration':empty,'revision':revision,'confirm':False},csrf);assert status==422
    status,body=request('/api/v1/configuration/import',{'configuration':empty,'revision':revision,'confirm':True},csrf);assert status==200,(status,body)
    status,_=request('/api/v1/configuration/import',{'configuration':empty,'revision':revision,'confirm':True},csrf);assert status==409
    cleared=json.loads(request('/api/v1/services')[1]);assert cleared['services']==[] and cleared['published']==before['published']
    assert json.loads(request('/api/v1/draft/preview')[1])['runtime_hash']==runtime,'draft import wrote to Caddy'
    status,body=request('/api/v1/configuration/import',{'configuration':configuration,'revision':cleared['revision'],'confirm':True},csrf);assert status==200,(status,body)
    restored=json.loads(request('/api/v1/services')[1]);assert [s['hostname'] for s in restored['services']]==[s['hostname'] for s in before['services']]
    status,body=request('/api/v1/draft/preview');assert status==200,(status,body);preview=json.loads(body)
    status,body=request('/api/v1/draft/validate',{'revision':preview['revision']},csrf)
    if bootstrap:
        assert status==409 and '公网可信证书' in body,(status,body)
        return None
    assert status==200,(status,body);preview=json.loads(body)
    status,body=request('/api/v1/deployments',{'validation_id':preview['validation_id'],'revision':preview['revision'],
        'expected_hash':preview['runtime_hash'],'idempotency_key':uuid.uuid4().hex,'confirm_drift':True},csrf);assert status==202,(status,body)
    deployment=json.loads(body)['id']
    eventually(lambda:json.loads(request('/api/v1/deployments/'+deployment)[1])['deployment']['status']=='success')
    return deployment

try:
    compose('up','-d','--build')
    compose_ids=compose('ps','-q','caddy').stdout.strip()
    mount_targets=json.loads(docker('inspect',compose_ids).stdout)[0]['Mounts']
    assert {m['Destination'] for m in mount_targets}=={'/var/lib/manager','/srv/snapshots','/data','/config'},mount_targets
    binding=[]
    eventually(lambda:(binding.append(compose('port','caddy','443','--protocol','tcp',check=False).stdout.strip()) or bool(binding[-1])))
    setup_address=binding[-1].splitlines()[0].replace('0.0.0.0:', '127.0.0.1:').replace('[::]:', '[::1]:')
    setup_url='https://'+setup_address+'/api/v1/setup/status'
    setup_status=run('curl','--noproxy','*','-sk','--connect-timeout','2','--max-time','8','-w','\n%{http_code}',setup_url)
    _,status=setup_status.stdout.rsplit('\n',1)
    assert int(status)==200,('Compose mapped HTTPS port did not expose anonymous setup status',status,setup_status.stdout)
    compose('down','-v')

    docker('network','create',network)
    probe=prefix+'-probe';containers.append(probe)
    docker('run','-d','--name',probe,'--network',network,'--entrypoint','sh',image,'-ec','sleep 3600')
    eventually(lambda:docker('exec',probe,'curl','--version',check=False).returncode==0)
    upstream_ip=docker('inspect','-f','{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}',probe).stdout.strip()
    imported_services=[{'name':'Imported draft','group':'homelab','hostname':'imported.home.example.test','scheme':'http','host':upstream_ip,'port':8088,'enabled':False,'notes':'Isolated import fixture'}]
    embedded_values={}
    embedded_mounts=data('embedded')
    embedded=start('embedded',embedded_mounts,embedded_values)
    _,hostname,processes=setup(embedded,embedded_values,imported_services)
    assert 'caddy run' in processes and 'manager serve' in processes,processes
    request=api_client(embedded,hostname,embedded)
    exercise(request,bootstrap=True)
    docker('restart','-t','20',embedded)
    eventually(lambda:docker('exec',embedded,'manager','health',check=False).returncode==0)
    assert 'manager serve' in docker('top',embedded,'-eo','pid,args').stdout
    exercise(request,bootstrap=True)
    print('PASS embedded: 80/443 blank-volume setup, plaintext POST rejection, first-writer conflict, same-IP handoff, unknown-host isolation, HTTPS login, configuration import/export and restart recovery',flush=True)

    external_mounts=data('external-manager')
    bootstrap_host='caddyadmin.home.example.test'
    remote_cfg={'admin':{'listen':'0.0.0.0:2019','config':{'persist':True}},'storage':{'module':'file_system','root':'/data/remote-certs'},
      'apps':{'http':{'servers':{'bootstrap':{'listen':[':443'],'routes':[{'match':[{'host':[bootstrap_host]}],
        'handle':[{'handler':'reverse_proxy','upstreams':[{'dial':'controller:8080'}],
        'headers':{'request':{'set':{'X-Caddy-Client-IP':['{http.request.remote.host}']}}}}]}],'tls_connection_policies':[{}]}}},
        'tls':{'certificates':{'automate':[bootstrap_host]},'automation':{'policies':[{'issuers':[{'module':'internal'}]}]}},
        'pki':{'certificate_authorities':{'local':{'install_trust':False}}}}}
    remote_file=root/'external.json';remote_file.write_text(json.dumps(remote_cfg));remote_file.chmod(0o644)
    remote_mounts=data('remote')+['-v',str(remote_file)+':/tmp/bootstrap.json:ro']
    remote_values={'TEST_TLS':'true'}
    remote=start('remote',remote_mounts,remote_values,alias='external',command=('caddy','run','--config','/tmp/bootstrap.json'))
    external_values={'TEST_TLS':'true','CADDY_ADMIN_URL':'http://external:2019',
        'MANAGER_DIAL':'controller:8080','CADDY_PROBE_ADDRESS':'external:443'}
    manager=start('manager',external_mounts,external_values,alias='controller')
    _,_,processes=setup(manager,external_values,imported_services)
    assert 'caddy run' not in processes and 'manager serve' in processes,processes
    request=api_client(remote,bootstrap_host,remote)
    exercise(request)
    current=json.loads(docker('exec',remote,'wget','-qO-','http://127.0.0.1:2019/config/').stdout)
    assert current['admin']==remote_cfg['admin'] and current['storage']==remote_cfg['storage']
    docker('restart','-t','20',manager);eventually(lambda:docker('exec',manager,'manager','health',check=False).returncode==0)
    docker('stop','-t','20',remote);assert docker('exec',manager,'manager','health',check=False).returncode!=0
    assert 'caddy run' not in docker('top',manager,'-eo','pid,args').stdout
    docker('rm','-f',remote)
    remote=start('remote',remote_mounts,remote_values,alias='external',command=('caddy','run','--resume'))
    eventually(lambda:docker('exec',remote,'wget','-qO-','http://127.0.0.1:2019/config/',check=False).returncode==0)
    eventually(lambda:docker('exec',manager,'manager','health',check=False).returncode==0)
    resumed=json.loads(docker('exec',remote,'wget','-qO-','http://127.0.0.1:2019/config/').stdout)
    assert resumed==current,'external restart lost published config'
    print('PASS external: setup-only mode, no local Caddy, preserved remote admin/storage, configuration import/export, HTTPS publish, restart and no fallback on outage',flush=True)
finally:
    compose('down','-v',check=False)
    for container in reversed(containers):docker('rm','-f',container,check=False)
    for volume in volumes:docker('volume','rm',volume,check=False)
    docker('network','rm',network,check=False)
    # Runtime directories intentionally belong to container UID 10001 with mode 0700.
    # Remove their contents inside the container namespace before TemporaryDirectory
    # performs its host-side cleanup (including on user-namespace-remapped Docker).
    docker('run','--rm','--entrypoint','find','-v',str(root)+':/cleanup',image,
           '/cleanup','-mindepth','1','-depth','-delete',check=False)
    work.cleanup()
