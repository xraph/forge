export function UnknownIntent({ intent }: { intent: string }) {
  return (
    <div className="rounded border border-dashed border-amber-400 bg-amber-50 p-3 text-sm text-amber-900">
      Unknown intent: <code>{intent}</code>
    </div>
  );
}

export function LoadingNode() {
  return (
    <div className="rounded border border-gray-200 bg-gray-100 p-3 text-sm text-gray-500">
      Loading…
    </div>
  );
}

export function ErrorNode({ message }: { message: string }) {
  return (
    <div className="rounded border border-red-300 bg-red-50 p-3 text-sm text-red-900">
      Error: {message}
    </div>
  );
}
