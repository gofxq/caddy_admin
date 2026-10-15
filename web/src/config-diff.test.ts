import {describe,expect,it} from 'vitest'
import {diffJSON,formatJSON} from './config-diff'

describe('configuration diff',()=>{
 it('ignores object key order while preserving array order',()=>{
  expect(diffJSON({b:2,a:{z:1,x:0}},{a:{x:0,z:1},b:2}).lines.every(l=>l.kind==='context')).toBe(true)
  expect(diffJSON({routes:['a','b']},{routes:['b','a']}).lines.some(l=>l.kind==='added')).toBe(true)
 })
 it('highlights an upstream replacement and keeps before and after line numbers',()=>{
  const diff=diffJSON({upstream:'10.0.0.8:80'},{upstream:'10.0.0.9:80'})
  expect(diff.lines).toEqual([
   {kind:'context',text:'{',before:1,after:1},
   {kind:'removed',text:'  "upstream": "10.0.0.8:80"',before:2},
   {kind:'added',text:'  "upstream": "10.0.0.9:80"',after:2},
   {kind:'context',text:'}',before:3,after:3},
  ])
  expect(diff.simplified).toBe(false)
 })
 it('isolates inserted routes instead of marking later routes changed',()=>{
  const before={routes:[{host:'a'},{host:'z'}]},after={routes:[{host:'a'},{host:'b'},{host:'z'}]}
  const diff=diffJSON(before,after)
  expect(diff.lines.filter(l=>l.kind==='removed')).toHaveLength(0)
  expect(diff.lines.filter(l=>l.kind==='added').map(l=>l.text)).toEqual(['      "host": "b"','    },','    {'])
 })
 it('reconstructs both sides for empty, nested, repeated and unrelated JSON',()=>{
  const inputs:unknown[]=[null,{},[],{a:null,b:true},[1,1,1,2],[1,2,1,1],{z:[{},[],{}]},'中文 <script>']
  for(const before of inputs)for(const after of inputs){
   const {lines}=diffJSON(before,after)
   expect(lines.filter(l=>l.kind!=='added').map(l=>l.text).join('\n')).toBe(formatJSON(before))
   expect(lines.filter(l=>l.kind!=='removed').map(l=>l.text).join('\n')).toBe(formatJSON(after))
  }
 })
 it('bounds work for a large completely replaced configuration without dropping content',()=>{
  const before=Array.from({length:1500},(_,i)=>`before-${i}`),after=before.map((_,i)=>`after-${i}`)
  const result=diffJSON(before,after)
  expect(result.simplified).toBe(true)
  expect(result.lines.filter(l=>l.kind!=='added').map(l=>l.text).join('\n')).toBe(formatJSON(before))
  expect(result.lines.filter(l=>l.kind!=='removed').map(l=>l.text).join('\n')).toBe(formatJSON(after))
 })
})
