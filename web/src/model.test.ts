import { describe,it,expect } from 'vitest'
import { serviceState, filterServices } from './model'
const service={id:'one',name:'Photos',group:'homelab',hostname:'photo.home.example.com',scheme:'http',host:'10.77.0.8',port:2283,enabled:true,notes:'',dial:'10.77.0.8:2283',updated_at:''} as const
 describe('draft visibility',()=>{
 it('distinguishes saved upstream changes from live configuration',()=>{expect(serviceState({...service,port:2284},[service])).toBe('未发布修改');expect(serviceState(service,[service])).toBe('已生效');expect(serviceState(service,[])).toBe('待首次发布')})
 it('combines keyword, group and enabled filters',()=>{expect(filterServices([service],'photo','homelab','enabled')).toHaveLength(1);expect(filterServices([service],'photo','public','enabled')).toHaveLength(0)})
})
