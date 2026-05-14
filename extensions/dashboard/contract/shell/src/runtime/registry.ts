import type { ComponentType } from "react";
import type { GraphNode } from "../contract/types";

export interface IntentComponentProps<TData = unknown, TProps = Record<string, unknown>> {
  node: GraphNode;
  data?: TData;
  props: TProps;
  slots: Record<string, GraphNode[]>;
}

export type IntentComponent = ComponentType<IntentComponentProps<unknown, Record<string, unknown>>>;

export class IntentRegistry {
  private byName = new Map<string, IntentComponent>();

  register(name: string, component: IntentComponent): this {
    if (this.byName.has(name)) {
      throw new Error(`intent ${name} already registered`);
    }
    this.byName.set(name, component);
    return this;
  }

  resolve(name: string): IntentComponent | undefined {
    return this.byName.get(name);
  }

  has(name: string): boolean {
    return this.byName.has(name);
  }
}
