export const isStaticDemo=import.meta.env.MODE==='demo'

// Setup and the authenticated client share this boundary. Demo requests never
// fall back to fetch, including requests unsupported by the simulator.
export function request(path:string,options:RequestInit={}):Promise<Response>{
 if(import.meta.env.MODE==='demo')return import('./demo/runtime').then(({demoRequest})=>demoRequest(path,options))
 return fetch(path,options)
}
