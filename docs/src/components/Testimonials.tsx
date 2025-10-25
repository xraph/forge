import { testimonials } from "@/constants/testimonials";

export const Testimonials = () => {
  return (
    <section className="relative py-32 bg-secondary/30">
      <div className="container relative mx-auto px-4 sm:px-6 lg:px-8">
        <div className="max-w-3xl mb-20">
          <div className="text-sm font-semibold tracking-wide text-muted-foreground mb-4">
            WHAT EVERYONE IS SAYING
          </div>
          <h2 className="text-5xl sm:text-6xl font-black mb-6">
            Trusted by professionals.
          </h2>
        </div>
        
        <div className="grid md:grid-cols-3 gap-8">
          {testimonials.map((testimonial, index) => (
            <div 
              key={index}
              className="p-8 rounded-2xl border border-border bg-card"
            >
              <p className="text-lg mb-8 leading-relaxed">
                "{testimonial.quote}"
              </p>
              <div>
                <div className="font-bold text-foreground">{testimonial.author}</div>
                <div className="text-sm text-muted-foreground">
                  {testimonial.role}, {testimonial.company}
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
};
