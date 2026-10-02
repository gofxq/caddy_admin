export function validNewPassword(value:string){
 return Array.from(value).length>=8&&new TextEncoder().encode(value).length<=256
}

export function generatePassword(){
 const alphabet='ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-'
 const bytes=new Uint8Array(20)
 crypto.getRandomValues(bytes)
 return Array.from(bytes,byte=>alphabet[byte&63]).join('')
}
