import { useIntentRegistry } from "./context";
import { UnknownIntent } from "./fallbacks";
import type { GraphNode } from "../contract/types";

export function GraphRenderer({ node }: { node: GraphNode }) {
  const registry = useIntentRegistry();
  const Component = registry.resolve(node.intent);
  if (!Component) return <UnknownIntent intent={node.intent} />;
  return (
    <Component node={node} props={node.props ?? {}} slots={node.slots ?? {}} data={undefined} />
  );
}
