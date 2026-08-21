import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "sonner";
import { useEffect } from "react";
import { useTheme } from "@/stores/theme";

const client = new QueryClient({
  defaultOptions: { queries: { staleTime: 30_000, retry: 1, refetchOnWindowFocus: false } }
});

export function Providers({ children }: { children: ReactNode }) {
  const theme = useTheme((s) => s.theme);
  useEffect(() => {
    useTheme.getState().setTheme(theme);
  }, [theme]);
  return (
    <QueryClientProvider client={client}>
      <TooltipProvider delayDuration={300}>
        {children}
        <Toaster theme={theme === "light" ? "light" : "dark"} richColors position="bottom-right" />
      </TooltipProvider>
    </QueryClientProvider>
  );
}
