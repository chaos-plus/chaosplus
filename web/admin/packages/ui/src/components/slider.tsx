import * as React from "react"
import { Slider } from "@base-ui/react/slider"

import { cn } from "@workspace/ui/lib/utils"

interface SliderProps extends React.ComponentPropsWithoutRef<
  typeof Slider.Root
> {
  className?: string
}

const SliderRoot = React.forwardRef<
  React.ElementRef<typeof Slider.Root>,
  SliderProps
>(
  (
    {
      className,
      value,
      defaultValue,
      thumbCollisionBehavior = "none",
      ...props
    },
    ref
  ) => {
    const thumbCount = Array.isArray(value)
      ? value.length
      : Array.isArray(defaultValue)
        ? defaultValue.length
        : 1

    return (
      <Slider.Root
        ref={ref}
        data-slot="slider"
        className={cn(
          "relative flex w-full touch-none items-center select-none",
          className
        )}
        value={value}
        defaultValue={defaultValue}
        thumbCollisionBehavior={thumbCollisionBehavior}
        {...props}
      >
        <Slider.Control className="relative flex w-full items-center">
          <Slider.Track className="relative h-2 w-full grow overflow-hidden rounded-full bg-secondary">
            <Slider.Indicator className="absolute h-full bg-primary" />
          </Slider.Track>
          {Array.from({ length: thumbCount }).map((_, index) => (
            <Slider.Thumb
              key={index}
              index={index}
              className="block h-5 w-5 rounded-full border-2 border-primary bg-background ring-offset-background transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50"
            />
          ))}
        </Slider.Control>
      </Slider.Root>
    )
  }
)
SliderRoot.displayName = "Slider"

export { SliderRoot as Slider }
