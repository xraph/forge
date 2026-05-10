import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { IntentRegistry } from "../src/runtime/registry";
import { IntentRegistryProvider } from "../src/runtime/context";
import { GraphRenderer } from "../src/runtime/renderer";
import { SlotRenderer } from "../src/runtime/slots";
import type { GraphNode } from "../src/contract/types";

function setup(node: GraphNode, registry: IntentRegistry) {
  return render(
    <IntentRegistryProvider value={registry}>
      <GraphRenderer node={node} />
    </IntentRegistryProvider>,
  );
}

describe("GraphRenderer", () => {
  it("renders a registered intent's component", () => {
    const reg = new IntentRegistry();
    reg.register("hello", () => <p>hello world</p>);
    setup({ intent: "hello" }, reg);
    expect(screen.getByText("hello world")).toBeInTheDocument();
  });

  it("renders UnknownIntent fallback when intent is not registered", () => {
    const reg = new IntentRegistry();
    setup({ intent: "missing" }, reg);
    expect(screen.getByText(/Unknown intent/i)).toBeInTheDocument();
    expect(screen.getByText("missing")).toBeInTheDocument();
  });

  it("recursively renders slot children via SlotRenderer", () => {
    const reg = new IntentRegistry();
    reg.register("parent", ({ slots }) => (
      <div data-testid="parent">
        <SlotRenderer slot="main" slots={slots} />
      </div>
    ));
    reg.register("leaf", ({ node }) => <span>leaf-{node.intent}</span>);
    const node: GraphNode = {
      intent: "parent",
      slots: { main: [{ intent: "leaf" }, { intent: "leaf" }] },
    };
    setup(node, reg);
    expect(screen.getByTestId("parent")).toBeInTheDocument();
    expect(screen.getAllByText(/leaf-leaf/)).toHaveLength(2);
  });
});

describe("IntentRegistry", () => {
  it("rejects double registration", () => {
    const reg = new IntentRegistry();
    reg.register("x", () => null);
    expect(() => reg.register("x", () => null)).toThrow(/already registered/);
  });
});
