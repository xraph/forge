import { useCases } from "@/constants/useCases";

export const UseCases = () => {
  return (
    <section className="relative py-32 overflow-hidden">
      {/* Gradient Background */}
      <div className="absolute inset-0 gradient-mesh opacity-30" />
      
      <div className="container relative mx-auto px-4 sm:px-6 lg:px-8">
        <div className="text-center max-w-3xl mx-auto mb-20">
          <div className="text-sm font-semibold tracking-wide text-muted-foreground mb-4">
            USE CASES
          </div>
          <h2 className="text-5xl sm:text-6xl lg:text-7xl font-black mb-6 leading-tight">
            Built for any backend.
          </h2>
          <p className="text-xl text-muted-foreground leading-relaxed">
            From microservices to monoliths, Forge adapts to your needs.
          </p>
        </div>

        <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-6">
          {useCases.map((useCase, index) => (
            <div
              key={index}
              className="group p-8 rounded-3xl border border-border bg-card hover:shadow-2xl hover:border-primary/50 transition-all duration-300"
            >
              <div className="mb-6 inline-flex p-4 rounded-2xl bg-gradient-to-br from-primary/10 to-accent/10 border border-primary/20 group-hover:scale-110 transition-transform">
                <useCase.icon className="w-8 h-8 text-primary" />
              </div>
              <h3 className="text-2xl font-black mb-3">
                {useCase.title}
              </h3>
              <p className="text-muted-foreground mb-4 leading-relaxed">
                {useCase.description}
              </p>
              <div className="inline-flex items-center gap-2 px-3 py-1 bg-secondary rounded-full text-xs font-semibold">
                {useCase.stats}
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
};
