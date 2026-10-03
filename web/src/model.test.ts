import { describe,it,expect } from 'vitest'
import { serviceState, filterServices } from './model'
const service={id:'one',name:'Photos',domain_id:'home',hostname:'photo.home.example.com',scheme:'http',host:'10.77.0.8',port:2283,enabled:true,notes:'',dial:'10.77.0.8:2283',updated_at:''} as const
 describe('draft visibility',()=>{
 it('distinguishes saved upstream changes from live configuration',()=>{expect(serviceState({...service,port:2284},[service])).toBe('未发布修改');expect(serviceState(service,[service])).toBe('已生效');expect(serviceState(service,[])).toBe('待首次发布')})
 it('combines keyword, group and enabled filters',()=>{expect(filterServices([service],'photo','home','enabled')).toHaveLength(1);expect(filterServices([service],'photo','public','enabled')).toHaveLength(0)})
})
it('requires confirmation when trusted networks widen for the console or trusted business domains',async()=>{
 const {exposureChanges}=await import('./model')
 const before={origin:'https://caddyadmin.example.com',admin_domain:'caddyadmin.example.com',domains:[{id:'first',name:'example.com',access:null}],console_lan_only:true,lan_cidrs:['10.0.0.0/8'],upstream_cidrs:[],allowed_names:[],denied_ips:[],resolvers:['1.1.1.1']} as import('./model').ManagedSettings
 expect(exposureChanges(before,{...before,lan_cidrs:['0.0.0.0/0']})).toBe(true)
 expect(exposureChanges(before,{...before,lan_cidrs:[]})).toBe(false)
 expect(exposureChanges({...before,console_lan_only:false},{...before,console_lan_only:false,lan_cidrs:['0.0.0.0/0']})).toBe(false)
 expect(exposureChanges({...before,console_lan_only:false,domains:[{id:'first',name:'example.com',access:'trusted'}]},{...before,console_lan_only:false,domains:[{id:'first',name:'example.com',access:'trusted'}],lan_cidrs:['0.0.0.0/0']})).toBe(true)
})
