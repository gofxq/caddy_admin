import { describe, expect, it } from 'vitest'
import { createMockApi } from '../mock-api.ts'
import type { Deployment, Preview, Service, ServiceList } from './model'

describe('local mock API', () => {
  it('enforces the new password minimum without ending a session on rejected changes',()=>{
    const mock=createMockApi()
    expect(mock.handle('POST','/auth/password',{current:'demo',password:'1234567'}).status).toBe(422)
    expect(mock.handle('GET','/auth/session').status).toBe(200)
    expect(mock.handle('POST','/auth/password',{current:'demo',password:'12345678'}).status).toBe(204)
    expect(mock.handle('GET','/auth/session').status).toBe(401)
  })
  it('serves every main view without a backend', () => {
    const mock = createMockApi()
    for (const path of ['/auth/session', '/overview', '/services', '/draft/preview', '/deployments', '/certificates', '/audit', '/settings']) {
      expect(mock.handle('GET', path).status, path).toBe(200)
    }
    expect(mock.handle('GET', '/missing').status).toBe(404)
  })

  it('keeps service edits and publishing in one in-memory state', () => {
    const mock = createMockApi()
    const initial = mock.handle('GET', '/services').body as ServiceList
    const service: Service = { ...initial.services[0], id: '', name: '测试服务', hostname: 'test.home.example.com' }
    expect(mock.handle('POST', '/services', { revision: initial.revision, service }).status).toBe(200)
    const edited = mock.handle('GET', '/services').body as ServiceList
    expect(edited.revision).toBe(initial.revision + 1)
    expect(edited.services.some(item => item.hostname === service.hostname)).toBe(true)
    expect(edited.published.some(item => item.hostname === service.hostname)).toBe(false)
    expect(mock.handle('POST', '/services', { revision: initial.revision, service }).status).toBe(409)

    const checked = mock.handle('POST', '/draft/validate', { revision: edited.revision, rollback_id: '' }).body as Preview
    expect(checked.validation_id).toBeTruthy()
    const release = mock.handle('POST', '/deployments', { validation_id: checked.validation_id, revision: checked.revision, expected_hash: checked.runtime_hash }).body as Deployment
    expect(release.status).toBe('applying')
    mock.handle('GET', `/deployments/${release.id}`)
    const finished=mock.handle('GET', `/deployments/${release.id}`).body as {deployment:Deployment}
    expect(finished.deployment.status).toBe('success')
    expect((mock.handle('GET', '/services').body as ServiceList).published.some(item => item.hostname === service.hostname)).toBe(true)
    expect((mock.handle('GET', '/draft/preview').body as Preview).changes).toHaveLength(0)
  })

  it('can simulate failed and uncertain terminal deployments',()=>{
    for(const [key,status] of [['mock-failed','failed'],['mock-uncertain','uncertain']] as const){
      const mock=createMockApi()
      const services=mock.handle('GET','/services').body as ServiceList
      const checked=mock.handle('POST','/draft/validate',{revision:services.revision,rollback_id:''}).body as Preview
      const release=mock.handle('POST','/deployments',{validation_id:checked.validation_id,revision:checked.revision,expected_hash:checked.runtime_hash,idempotency_key:key}).body as Deployment
      expect(release.status).toBe('applying')
      mock.handle('GET',`/deployments/${release.id}`)
      const result=mock.handle('GET',`/deployments/${release.id}`).body as {deployment:Deployment}
      expect(result.deployment.status).toBe(status)
    }
  })

  it('lets a demo logout reach the login page', () => {
    const mock = createMockApi()
    expect(mock.handle('POST', '/auth/logout').status).toBe(204)
    expect(mock.handle('GET', '/auth/session').status).toBe(401)
    expect(mock.handle('POST', '/auth/login', { username: 'admin', password: 'demo' }).status).toBe(200)
    expect(mock.handle('GET', '/auth/session').status).toBe(200)
  })
})

it('exports portable configuration and imports only drafts with confirmation and revision protection',()=>{
 const mock=createMockApi();const initial=mock.handle('GET','/services').body as ServiceList
 const exported=mock.handle('GET','/configuration/export');expect(exported.status).toBe(200)
 const configuration=exported.body as {services:unknown[];version:number};expect(JSON.stringify(configuration)).not.toMatch(/"(dial|id|updated_at|token|password)"/)
 configuration.services=[]
 expect(mock.handle('POST','/configuration/preview',{configuration}).status).toBe(200)
 expect(mock.handle('POST','/configuration/import',{configuration,revision:initial.revision,confirm:false}).status).toBe(422)
 expect(mock.handle('POST','/configuration/import',{configuration,revision:initial.revision,confirm:true}).status).toBe(200)
 const changed=mock.handle('GET','/services').body as ServiceList;expect(changed.services).toEqual([]);expect(changed.published).toEqual(initial.published);expect(changed.revision).toBe(initial.revision+1)
 expect(mock.handle('POST','/configuration/import',{configuration,revision:initial.revision,confirm:true}).status).toBe(409)
 expect(mock.handle('POST','/configuration/preview',{configuration:{...configuration,version:2}}).status).toBe(422)
})

it('reports initialized demo state and never performs Setup DNS writes',()=>{
 const mock=createMockApi()
 expect(mock.handle('GET','/setup/status').body).toMatchObject({initialized:true,external_caddy:false})
 expect(mock.handle('POST','/setup/dns/preview',{token:'demo-token'}).status).toBe(409)
 expect(mock.handle('POST','/setup/complete',{}).status).toBe(409)
})
