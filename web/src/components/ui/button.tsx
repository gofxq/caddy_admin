import { Button as ButtonPrimitive } from '@base-ui/react/button'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'
const buttonVariants=cva('button',{variants:{variant:{default:'button-primary',outline:'button-outline',ghost:'button-ghost',destructive:'button-danger'},size:{default:'',sm:'button-sm'}},defaultVariants:{variant:'default',size:'default'}})
export function Button({className,variant,size,...props}:ButtonPrimitive.Props & VariantProps<typeof buttonVariants>){return <ButtonPrimitive className={cn(buttonVariants({variant,size}),className)} {...props}/>}
