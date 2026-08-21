import { Component, type ReactNode } from "react";
import { Button } from "@/components/ui/button";

export class ErrorBoundary extends Component<{ children: ReactNode }, { err?: Error }> {
  state: { err?: Error } = {};
  static getDerivedStateFromError(err: Error) {
    return { err };
  }
  render() {
    if (this.state.err) {
      return (
        <div className="flex min-h-dvh flex-col items-center justify-center p-6 text-center">
          <h1 className="text-2xl font-semibold">Something went wrong</h1>
          <p className="mt-2 max-w-md text-sm text-muted">{this.state.err.message}</p>
          <Button className="mt-4" onClick={() => location.reload()}>Reload</Button>
        </div>
      );
    }
    return this.props.children;
  }
}
