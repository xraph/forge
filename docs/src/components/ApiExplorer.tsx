'use client'

import { useState } from "react";
import { apiEndpoints } from "@/constants/apiExplorer";
import { Button } from "@/components/ui/button";
import { SectionHeader } from "@/components/ui/section-header";
import { MethodBadge } from "@/components/ui/method-badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ScrollArea } from "@/components/ui/scroll-area";

export const ApiExplorer = () => {
  const [selectedEndpoint, setSelectedEndpoint] = useState(0);
  const endpoint = apiEndpoints[selectedEndpoint];

  return (
    <section className="relative py-32 overflow-hidden">
      <div className="absolute inset-0 gradient-mesh opacity-20" />
      
      <div className="container relative mx-auto px-4 sm:px-6 lg:px-8">
        <SectionHeader
          title="Explore the SDK."
          description="Learn how to use Forge Framework and Forge CLI through interactive examples."
        />

        <div className="max-w-6xl mx-auto">
          <div className="grid lg:grid-cols-4 gap-6">
            {/* Endpoints List */}
            <div className="lg:col-span-1">
              <ScrollArea className="h-[600px] pr-4">
                <div className="space-y-3 px-1 py-1">
                  {apiEndpoints.map((ep, index) => (
                    <button
                      key={index}
                      onClick={() => setSelectedEndpoint(index)}
                      className={`group w-full text-left p-4 rounded-xl border-2 transition-all duration-300 ${
                        selectedEndpoint === index
                          ? "border-primary bg-gradient-to-br from-primary/10 via-primary/5 to-transparent shadow-lg shadow-primary/5"
                          : "border-border/40 bg-card/80 backdrop-blur-sm hover:border-primary/40 hover:bg-card hover:shadow-md hover:-translate-y-0.5"
                      }`}
                    >
                      <div className="flex items-center gap-2.5 mb-2.5">
                        <MethodBadge method={ep.method} size="sm" />
                        <div className={`ml-auto w-1.5 h-1.5 rounded-full transition-all ${
                          selectedEndpoint === index 
                            ? "bg-primary scale-100" 
                            : "bg-muted-foreground/20 scale-0 group-hover:scale-100"
                        }`} />
                      </div>
                      <p className="text-xs font-mono font-semibold text-foreground/80 group-hover:text-foreground truncate mb-2 transition-colors">{ep.path}</p>
                      <p className="text-xs text-muted-foreground leading-relaxed line-clamp-2">{ep.description}</p>
                    </button>
                  ))}
                </div>
              </ScrollArea>
            </div>

            {/* Endpoint Details */}
            <div className="lg:col-span-3">
              <div className="rounded-2xl border border-border bg-card/50 backdrop-blur-sm overflow-hidden shadow-lg">
                {/* Header */}
                <div className="px-6 py-5 border-b border-border bg-muted/30">
                  <div className="flex items-center gap-3 flex-wrap mb-2">
                    <MethodBadge method={endpoint.method} />
                    <code className="text-sm font-mono px-3 py-1 bg-secondary/50 rounded">{endpoint.path}</code>
                  </div>
                  <p className="text-sm text-muted-foreground">{endpoint.description}</p>
                </div>

                {/* Tabs for different sections */}
                <Tabs defaultValue="details" className="w-full">
                  <div className="px-6 border-b border-border">
                    <TabsList className="bg-transparent h-12 p-0 gap-6">
                      <TabsTrigger 
                        value="details" 
                        className="data-[state=active]:bg-transparent data-[state=active]:shadow-none border-b-2 border-transparent data-[state=active]:border-primary rounded-none px-0"
                      >
                        Details
                      </TabsTrigger>
                      {endpoint.body && (
                        <TabsTrigger 
                          value="request" 
                          className="data-[state=active]:bg-transparent data-[state=active]:shadow-none border-b-2 border-transparent data-[state=active]:border-primary rounded-none px-0"
                        >
                          Request
                        </TabsTrigger>
                      )}
                      <TabsTrigger 
                        value="response" 
                        className="data-[state=active]:bg-transparent data-[state=active]:shadow-none border-b-2 border-transparent data-[state=active]:border-primary rounded-none px-0"
                      >
                        Response
                      </TabsTrigger>
                    </TabsList>
                  </div>

                  <ScrollArea className="h-[400px]">
                    {/* Details Tab */}
                    <TabsContent value="details" className="m-0 p-6 space-y-6">
                      {endpoint.parameters && endpoint.parameters.length > 0 && (
                        <div>
                          <h4 className="font-semibold mb-4 text-sm uppercase tracking-wide text-muted-foreground">Parameters</h4>
                          <div className="space-y-3">
                            {endpoint.parameters.map((param, i) => (
                              <div key={i} className="p-3 rounded-lg bg-muted/50 border border-border/50">
                                <div className="flex items-center gap-3 mb-1">
                                  <code className="font-mono text-sm text-primary font-semibold">{param.name}</code>
                                  <span className="text-xs px-2 py-0.5 rounded bg-secondary text-secondary-foreground">{param.type}</span>
                                </div>
                                <p className="text-sm text-muted-foreground">{param.description}</p>
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                      {!endpoint.parameters && (
                        <p className="text-sm text-muted-foreground">No parameters required.</p>
                      )}
                    </TabsContent>

                    {/* Request Tab */}
                    {endpoint.body && (
                      <TabsContent value="request" className="m-0 p-6">
                        <div className="relative">
                          <div className="absolute top-3 right-3 text-xs text-muted-foreground font-mono">Go</div>
                          <pre className="text-sm bg-secondary/80 border border-border/50 p-4 rounded-xl overflow-x-auto font-mono leading-relaxed">
                            <code className="text-foreground/90">{endpoint.body}</code>
                          </pre>
                        </div>
                      </TabsContent>
                    )}

                    {/* Response Tab */}
                    <TabsContent value="response" className="m-0 p-6">
                      <div className="space-y-3">
                        <div className="flex items-center justify-between">
                          <h4 className="font-semibold text-sm uppercase tracking-wide text-muted-foreground">Response</h4>
                          <span className="text-xs px-3 py-1.5 rounded-full bg-green-500/10 text-green-600 dark:text-green-400 border border-green-500/20 font-semibold">
                            {endpoint.response.status}
                          </span>
                        </div>
                        <div className="relative">
                          <div className="absolute top-3 right-3 text-xs text-muted-foreground font-mono">JSON</div>
                          <pre className="text-sm bg-secondary/80 border border-border/50 p-4 rounded-xl overflow-x-auto font-mono leading-relaxed">
                            <code className="text-foreground/90">{endpoint.response.body || "No content"}</code>
                          </pre>
                        </div>
                      </div>
                    </TabsContent>
                  </ScrollArea>
                </Tabs>

                {/* Footer with action button */}
                <div className="px-6 py-4 border-t border-border bg-muted/30">
                  <Button className="w-full" size="lg">
                    Try it out
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
};
