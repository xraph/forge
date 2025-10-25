import { Code2, Zap, Eye, Shield, Puzzle } from "lucide-react";
import { bentoItems, codeExample } from "@/constants/bentoShowcase";

const itemComponents = {
  code: (item: typeof bentoItems[0]) => (
    <div className="relative h-full flex flex-col">
      <div className="mb-4">
        <h3 className="text-2xl font-black mb-2">{item.title}</h3>
        <p className="text-muted-foreground">{item.description}</p>
      </div>
      <div className="flex-1 rounded-xl bg-secondary/50 p-4 overflow-hidden">
        <pre className="text-xs sm:text-sm">
          <code>{codeExample}</code>
        </pre>
      </div>
    </div>
  ),
  metric: (item: typeof bentoItems[0]) => (
    <div className="flex flex-col items-center justify-center h-full text-center p-8">
      <div className="mb-4">
        <div className="text-6xl font-black bg-gradient-to-r from-primary to-accent bg-clip-text text-transparent">
          {item.metric}
        </div>
        <div className="text-sm text-muted-foreground mt-2">{item.label}</div>
      </div>
      <h3 className="text-xl font-black mb-2">{item.title}</h3>
      <p className="text-muted-foreground text-sm">{item.description}</p>
    </div>
  ),
  monitoring: (item: typeof bentoItems[0]) => (
    <div className="h-full flex flex-col">
      <h3 className="text-2xl font-black mb-2">{item.title}</h3>
      <p className="text-muted-foreground mb-4">{item.description}</p>
      <div className="flex-1 flex items-center justify-center">
        <Eye className="w-16 h-16 text-primary opacity-50" />
      </div>
    </div>
  ),
  security: (item: typeof bentoItems[0]) => (
    <div className="h-full flex flex-col items-center justify-center text-center p-8">
      <Shield className="w-16 h-16 text-primary mb-4" />
      <h3 className="text-xl font-black mb-2">{item.title}</h3>
      <p className="text-muted-foreground">{item.description}</p>
    </div>
  ),
  integrations: (item: typeof bentoItems[0]) => (
    <div className="h-full flex flex-col items-center justify-center text-center p-8">
      <Puzzle className="w-16 h-16 text-primary mb-4" />
      <h3 className="text-xl font-black mb-2">{item.title}</h3>
      <p className="text-muted-foreground">{item.description}</p>
    </div>
  )
};

const renderBentoItem = (item: typeof bentoItems[0]) => {
  const Component = itemComponents[item.type];
  if (!Component) {
    return null;
  }
  return <Component {...item} />;
};

export const BentoShowcase = () => {
  return (
    <section className="relative py-32 bg-background">
      <div className="container relative mx-auto px-4 sm:px-6 lg:px-8">
        <div className="text-center max-w-3xl mx-auto mb-20">
          <div className="text-sm font-semibold tracking-wide text-muted-foreground mb-4">
            POWERFUL FEATURES
          </div>
          <h2 className="text-5xl sm:text-6xl font-black mb-6">
            See what Forge can do.
          </h2>
          <p className="text-xl text-muted-foreground leading-relaxed">
            Explore the features that make Forge the perfect choice for your backend.
          </p>
        </div>

        <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-6">
          {bentoItems.map((item, index) => (
            <div
              key={index}
              className={`p-8 rounded-3xl border border-border bg-gradient-to-br ${item.gradient} hover:shadow-2xl transition-all duration-300 ${
                index === 0 ? 'md:col-span-2 lg:row-span-2' : ''
              }`}
            >
              {renderBentoItem(item)}
            </div>
          ))}
        </div>
      </div>
    </section>
  );
};
