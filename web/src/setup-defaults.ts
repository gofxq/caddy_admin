import {suggestedDNSAddress} from './components/SetupDNSGuidance'

export const DEFAULT_RESOLVER='1.1.1.1'
// A source address cannot reveal the actual subnet: these are editable hints.
export function suggestedNetworkCIDR(source:string){
 const address=suggestedDNSAddress(source)
 if(!address)return ''
 if(!address.includes(':'))return address.split('.').slice(0,3).join('.')+'.0/24'
 const [left,right='']=address.split('::')
 const before=left?left.split(':'):[],after=right?right.split(':'):[]
 const groups=address.includes('::')?[...before,...Array(8-before.length-after.length).fill('0'),...after]:before
 const network=[...groups.slice(0,4),'0','0','0','0'].join(':')
 return new URL(`http://[${network}]/`).hostname.slice(1,-1)+'/64'
}
