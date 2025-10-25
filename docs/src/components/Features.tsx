import { features } from "@/constants/features";

export const Features = () => {
  return (
    <section id="features" className="relative py-32 bg-background">
      <div className="container relative mx-auto px-4 sm:px-6 lg:px-8">
        <div className="max-w-3xl mb-20">
          <h2 className="text-5xl sm:text-6xl lg:text-7xl font-black mb-6 leading-tight">
            Everything you need. Nothing you don't.
          </h2>
          <p className="text-xl text-muted-foreground leading-relaxed">
            Forge brings together the best practices and patterns from modern backend development, 
            all in one opinionated framework.
          </p>
        </div>
        
        <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-8">
          {features.map((feature, index) => (
            <div 
              key={index}
              className="group p-8 rounded-2xl border border-border bg-card hover:shadow-lg transition-all duration-300"
            >
              <div className="mb-6 inline-flex p-3 rounded-xl bg-secondary">
                <feature.icon className="w-6 h-6" />
              </div>
              <h3 className="text-xl font-bold mb-3">
                {feature.title}
              </h3>
              <p className="text-muted-foreground leading-relaxed">
                {feature.description}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
};
