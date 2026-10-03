import React,{useEffect,useLayoutEffect} from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient,QueryClientProvider,useMutation,useQuery } from '@tanstack/react-query'
import { createRootRoute,createRoute,createRouter,RouterProvider,Outlet as RouterOutlet,Link } from '@tanstack/react-router'
import { Waypoints,LayoutDashboard,Server,GitBranch,ShieldCheck,ScrollText,Settings as SettingsIcon,LogOut,ArrowUpRight } from 'lucide-react'
import { api,setCSRF,APIError } from './api'
import type { Session } from './model'
import { Login,PasswordForm } from './pages/Login'
import { Overview } from './pages/Overview'
import { Services } from './pages/Services'
import { ServiceDetail } from './pages/ServiceDetail'
import { Deployments } from './pages/Deployments'
import { Certificates } from './pages/Certificates'
import { Audit } from './pages/Audit'
import { Settings } from './pages/Settings'
import { Setup } from './pages/Setup'
import { Button } from './components/ui/button'
import { Loading,ErrorBox } from './components/shared'
import { EntryMigration } from './components/EntryMigration'
import './styles.css'
const demoBanner=import.meta.env.MODE==='demo'?await import('./demo/Banner').then(({DemoBanner})=><DemoBanner/>):null

const queryClient=new QueryClient({defaultOptions:{queries:{retry:false,refetchOnWindowFocus:true},mutations:{retry:false}}})
async function readSession(){try{const s=await api<Session>('/auth/session');setCSRF(s.csrf);return s}catch(error){if(error instanceof APIError&&error.code==='unauthorized')return null;throw error}}
const protectedQueries={predicate:(q:{queryKey:readonly unknown[]})=>q.queryKey[0]!=='session'}

function Shell(){
 const session=useQuery({queryKey:['session'],queryFn:readSession,refetchInterval:60000})
 const logout=useMutation({mutationFn:()=>api('/auth/logout',{method:'POST',body:{}}),onSuccess:()=>{setCSRF('');queryClient.clear();void session.refetch()}})
 useEffect(()=>{const expired=()=>{setCSRF('');void queryClient.cancelQueries();queryClient.setQueryData(['session'],null)};window.addEventListener('session-expired',expired);return()=>window.removeEventListener('session-expired',expired)},[])
 // Remove caches after protected observers unmount; removing them while the
 // console still renders lets those observers recreate and fetch the queries.
 useLayoutEffect(()=>{if(session.data===null){void queryClient.cancelQueries(protectedQueries);queryClient.removeQueries(protectedQueries);queryClient.getMutationCache().clear()}},[session.data])

 if(session.isPending)return <>{demoBanner}<Loading/></>
 if(!session.data&&session.error)return <>{demoBanner}<div className="standalone"><ErrorBox error={session.error} onRetry={()=>void session.refetch()}/></div></>
 if(!session.data)return <>{demoBanner}<Login/></>
 if(session.data.must_change)return <>{demoBanner}<div className="standalone"><PasswordForm required onDone={()=>{queryClient.clear();session.refetch()}}/></div></>
 const nav=[{to:'/',label:'概览',icon:LayoutDashboard},{to:'/services',label:'服务',icon:Server},{to:'/deployments',label:'发布',icon:GitBranch},{to:'/certificates',label:'证书',icon:ShieldCheck},{to:'/audit',label:'审计',icon:ScrollText},{to:'/settings',label:'设置',icon:SettingsIcon}]
 return <div className="app-shell">
  <aside className="sidebar">
   <Link to="/" className="brand"><Waypoints size={29}/><span>Caddy<span className="brand-light"> Admin</span></span></Link>
   <div className="workspace"><span className="workspace-icon">H</span><div><strong>Homelab</strong><small>单实例控制台</small></div><ArrowUpRight size={16}/></div>
   <div className="nav-label">管理控制台</div><nav>{nav.map(n=><Link key={n.to} to={n.to} activeOptions={{exact:n.to!=='/services'}} activeProps={{className:'active'}}><n.icon size={19}/><span>{n.label}</span></Link>)}</nav>
   <div className="sidebar-bottom"><div className="user"><span className="avatar">{session.data.username.slice(0,1).toUpperCase()}</span><div><strong>{session.data.username}</strong><small>管理员</small></div><Button variant="ghost" aria-label="登出" disabled={logout.isPending} onClick={()=>logout.mutate()}><LogOut size={17}/></Button></div></div>
  </aside>
  <div className="main-column">{demoBanner}<div className="topbar"><span><span className="muted">工作空间</span> <span className="slash">/</span> Homelab</span><span className="topbar-label"><ShieldCheck size={14}/> Caddy Web Admin</span></div><main><EntryMigration/><ErrorBox error={logout.error} onRetry={()=>logout.mutate()} retryLabel="重新登出"/><Outlet/></main><footer><span>CADDY ADMIN · v0.1.0</span></footer></div>
 </div>
}
function Outlet(){const session=useQuery({queryKey:['session'],queryFn:readSession,enabled:false});return <>{session.data&&session.error&&<div><ErrorBox error={session.error} onRetry={()=>void queryClient.refetchQueries({queryKey:['session']})}/></div>}<RouterOutlet/></>}
const root=createRootRoute({component:Shell,notFoundComponent:()=> <div className="empty"><h1>页面不存在</h1><Link to="/">返回概览</Link></div>})
const serviceDetailRoute=createRoute({getParentRoute:()=>root,path:'/services/$id',component:()=>{const {id}=serviceDetailRoute.useParams();return <ServiceDetail key={id} id={id}/>}})
const routes=[serviceDetailRoute,createRoute({getParentRoute:()=>root,path:'/',component:Overview}),createRoute({getParentRoute:()=>root,path:'/services',component:Services}),createRoute({getParentRoute:()=>root,path:'/deployments',component:Deployments}),createRoute({getParentRoute:()=>root,path:'/certificates',component:Certificates}),createRoute({getParentRoute:()=>root,path:'/audit',component:Audit}),createRoute({getParentRoute:()=>root,path:'/settings',component:Settings})]
const router=createRouter({routeTree:root.addChildren(routes)})
declare module '@tanstack/react-router' {interface Register {router:typeof router}}
const application=location.pathname==='/setup'?<>{demoBanner}<Setup/></>:<QueryClientProvider client={queryClient}><RouterProvider router={router}/></QueryClientProvider>
ReactDOM.createRoot(document.getElementById('root')!).render(<React.StrictMode>{application}</React.StrictMode>)
