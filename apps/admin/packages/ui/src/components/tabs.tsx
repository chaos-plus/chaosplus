import * as React from "react"
import { Tabs } from "@base-ui/react/tabs"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@workspace/ui/lib/utils"

const tabsListVariants = cva(
  "inline-flex h-10 items-center justify-center rounded-md bg-muted p-1 text-muted-foreground",
  {
    variants: {
      variant: {
        default: "",
        line: "h-11 gap-4 rounded-none bg-transparent p-0 text-muted-foreground",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

const tabsTriggerVariants = cva(
  "inline-flex items-center justify-center rounded-sm px-3 py-1.5 text-sm font-medium whitespace-nowrap ring-offset-background transition-all focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50 data-[active]:bg-background data-[active]:text-foreground data-[active]:shadow-sm",
  {
    variants: {
      variant: {
        default: "",
        line: "relative rounded-none border-b-2 border-transparent bg-transparent px-1 pt-2 pb-3 shadow-none data-[active]:border-primary data-[active]:bg-transparent data-[active]:text-foreground data-[active]:shadow-none",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

const TabsRoot = Tabs.Root

const TabsList = React.forwardRef<
  React.ElementRef<typeof Tabs.List>,
  React.ComponentPropsWithoutRef<typeof Tabs.List> &
    VariantProps<typeof tabsListVariants>
>(({ className, variant, ...props }, ref) => (
  <Tabs.List
    ref={ref}
    className={cn(tabsListVariants({ variant }), className)}
    {...props}
  />
))
TabsList.displayName = Tabs.List.displayName ?? "TabsList"

const TabsTrigger = React.forwardRef<
  React.ElementRef<typeof Tabs.Tab>,
  React.ComponentPropsWithoutRef<typeof Tabs.Tab> &
    VariantProps<typeof tabsTriggerVariants>
>(({ className, variant, ...props }, ref) => (
  <Tabs.Tab
    ref={ref}
    className={cn(tabsTriggerVariants({ variant }), className)}
    {...props}
  />
))
TabsTrigger.displayName = Tabs.Tab.displayName ?? "TabsTrigger"

const TabsContent = React.forwardRef<
  React.ElementRef<typeof Tabs.Panel>,
  React.ComponentPropsWithoutRef<typeof Tabs.Panel>
>(({ className, ...props }, ref) => (
  <Tabs.Panel
    ref={ref}
    className={cn(
      "mt-2 ring-offset-background focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none",
      className
    )}
    {...props}
  />
))
TabsContent.displayName = Tabs.Panel.displayName ?? "TabsContent"

export { TabsRoot as Tabs, TabsList, TabsTrigger, TabsContent }
