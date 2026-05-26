import { AlertCircle, AlertTriangle } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";

export function UnknownIntent({ intent }: { intent: string }) {
  return (
    <Alert variant="muted">
      <AlertTriangle className="h-4 w-4" />
      <AlertTitle>Unknown intent</AlertTitle>
      <AlertDescription>
        The shell has no registered renderer for <code className="font-mono">{intent}</code>.
      </AlertDescription>
    </Alert>
  );
}

export function LoadingNode() {
  return (
    <div className="space-y-2">
      <Skeleton className="h-4 w-48" />
      <Skeleton className="h-4 w-32" />
      <Skeleton className="h-4 w-40" />
    </div>
  );
}

export function ErrorNode({ message }: { message: string }) {
  return (
    <Alert variant="destructive">
      <AlertCircle className="h-4 w-4" />
      <AlertTitle>Something went wrong</AlertTitle>
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  );
}
