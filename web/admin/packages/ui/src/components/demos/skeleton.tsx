import { Skeleton } from "../skeleton"
import { DemoSection } from "./_section"

export function SkeletonDemo() {
  return (
    <div className="space-y-8">
      <DemoSection titleKey="textLines">
        <div className="space-y-2">
          <Skeleton className="h-4 w-[250px]" />
          <Skeleton className="h-4 w-[200px]" />
          <Skeleton className="h-4 w-[280px]" />
        </div>
      </DemoSection>

      <DemoSection titleKey="card">
        <div className="flex items-center space-x-4">
          <Skeleton className="size-12 rounded-full" />
          <div className="space-y-2">
            <Skeleton className="h-4 w-[150px]" />
            <Skeleton className="h-4 w-[100px]" />
          </div>
        </div>
      </DemoSection>

      <DemoSection titleKey="form">
        <div className="w-full max-w-sm space-y-3">
          <Skeleton className="h-4 w-20" />
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-4 w-20" />
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-24" />
        </div>
      </DemoSection>
    </div>
  )
}
