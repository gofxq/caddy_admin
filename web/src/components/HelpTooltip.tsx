import {useId,useState,type ReactNode} from 'react'
import {Tooltip} from '@base-ui/react/tooltip'
import {CircleHelp} from 'lucide-react'

export function HelpTooltip({label,children}:{label:string;children:ReactNode}){
 const [open,setOpen]=useState(false)
 const id=useId()
 return <Tooltip.Root open={open} onOpenChange={setOpen} triggerId={id}>
  <Tooltip.Trigger id={id} type="button" className="help-trigger" aria-label={label} aria-describedby={open?id+'-help':undefined} delay={150} closeOnClick={false} onClick={()=>setOpen(true)}><CircleHelp size={16}/></Tooltip.Trigger>
  <Tooltip.Portal><Tooltip.Positioner className="help-positioner" side="top" sideOffset={8}><Tooltip.Popup id={id+'-help'} role="tooltip" className="help-tooltip">{children}</Tooltip.Popup></Tooltip.Positioner></Tooltip.Portal>
 </Tooltip.Root>
}
