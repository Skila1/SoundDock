import * as TabsPrimitive from "@radix-ui/react-tabs";
import { cn } from "@/lib/utils";

export const Tabs = TabsPrimitive.Root;

export function TabsList({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.List>) {
  return <TabsPrimitive.List className={cn("flex gap-1 rounded-full bg-surface-2/70 p-1 ring-1 ring-inset ring-border", className)} {...props} />;
}

export function TabsTrigger({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      className={cn("rounded-full px-3.5 py-1.5 text-sm font-medium text-muted transition-colors hover:text-foreground data-[state=active]:bg-surface-1 data-[state=active]:text-foreground data-[state=active]:shadow-sm", className)}
      {...props}
    />
  );
}

export const TabsContent = TabsPrimitive.Content;
