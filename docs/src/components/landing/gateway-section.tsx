'use client';

import { motion } from 'framer-motion';
import { Network, Server, Router, Globe, Shield, RefreshCw } from 'lucide-react';

const features = [
    { icon: Network, title: 'Zero-Config Discovery', desc: 'Services auto-register via mDNS/Bonjour. No manual catalogs needed.' },
    { icon: RefreshCw, title: 'FARP Protocol', desc: 'Automatic route configuration from OpenAPI/gRPC schemas.' },
    { icon: Shield, title: 'Production Ready', desc: 'Rate limiting, circuit breakers, and auth built-in.' },
];

function DiscoveryDiagram() {
    return (
        <div className="relative w-full h-[300px] bg-zinc-950/50 rounded-none border border-fd-border overflow-hidden p-8 flex items-center justify-center">
            {/* Background Grid */}
            <div className="absolute inset-0 bg-[linear-gradient(to_right,#80808012_1px,transparent_1px),linear-gradient(to_bottom,#80808012_1px,transparent_1px)] bg-[size:24px_24px]" />

            <div className="relative z-10 flex items-center gap-8 md:gap-16 w-full max-w-2xl justify-center">
                {/* Service */}
                <div className="flex flex-col items-center gap-3">
                    <div className="size-16 rounded-xl bg-blue-500/10 border border-blue-500/20 flex items-center justify-center text-blue-500 shadow-[0_0_30px_-10px_rgba(59,130,246,0.3)]">
                        <Server className="size-8" />
                    </div>
                    <div className="text-center">
                        <div className="text-sm font-semibold">Service A</div>
                        <div className="text-xs text-fd-muted-foreground mt-1 bg-fd-accent px-2 py-0.5 rounded-full">FARP Enabled</div>
                    </div>
                </div>

                {/* Connection Animation */}
                <div className="flex-1 h-[2px] bg-fd-border relative overflow-hidden">
                    <div className="absolute inset-0 bg-gradient-to-r from-transparent via-blue-500 to-transparent w-1/2 animate-[shimmer_2s_infinite]" />
                </div>

                {/* Gateway */}
                <div className="flex flex-col items-center gap-3">
                    <div className="size-20 rounded-2xl bg-purple-500/10 border border-purple-500/20 flex items-center justify-center text-purple-500 shadow-[0_0_40px_-10px_rgba(168,85,247,0.3)] relative">
                        <Router className="size-10" />
                        <div className="absolute -top-2 -right-2 size-5 bg-green-500 rounded-full border-4 border-zinc-950 flex items-center justify-center">
                            <div className="size-2 bg-white rounded-full animate-pulse" />
                        </div>
                    </div>
                    <div className="text-center">
                        <div className="text-base font-bold">Forge Gateway</div>
                        <div className="text-xs text-fd-muted-foreground">Auto-Configured</div>
                    </div>
                </div>

                {/* Connection Animation */}
                <div className="flex-1 h-[2px] bg-fd-border relative overflow-hidden">
                    <div className="absolute inset-0 bg-gradient-to-r from-transparent via-purple-500 to-transparent w-1/2 animate-[shimmer_2s_infinite_1s]" />
                </div>

                {/* Client */}
                <div className="flex flex-col items-center gap-3">
                    <div className="size-16 rounded-xl bg-zinc-500/10 border border-zinc-500/20 flex items-center justify-center text-fd-foreground">
                        <Globe className="size-8" />
                    </div>
                    <div className="text-center">
                        <div className="text-sm font-semibold">Public Internet</div>
                    </div>
                </div>
            </div>

            {/* Floating Labels */}
            <div className="absolute bottom-6 left-1/2 -translate-x-1/2 flex gap-4">
                <span className="text-[10px] uppercase tracking-wider text-fd-muted-foreground border border-fd-border px-2 py-1 rounded bg-fd-background/50 backdrop-blur">mDNS Discovery</span>
                <span className="text-[10px] uppercase tracking-wider text-fd-muted-foreground border border-fd-border px-2 py-1 rounded bg-fd-background/50 backdrop-blur">Reverse Proxy</span>
            </div>
        </div>
    );
}

export function GatewaySection() {
    return (
        <section className="border-b border-fd-border w-full ">
            <div className="container max-w-(--fd-layout-width) mx-auto px-6 py-16 md:py-24">
                <div className="grid lg:grid-cols-2 gap-12 lg:gap-20 items-center">
                    <motion.div
                        initial={{ opacity: 0, x: -20 }}
                        whileInView={{ opacity: 1, x: 0 }}
                        viewport={{ once: true }}
                        transition={{ duration: 0.6 }}
                    >
                        <div className="inline-flex items-center gap-2 border border-purple-500/30 bg-purple-500/10 px-3 py-1 text-xs font-medium text-purple-600 dark:text-purple-400 mb-6 rounded-none w-fit">
                            <Network className="size-3" />
                            Gateway & Discovery
                        </div>

                        <h2 className="text-3xl font-bold tracking-tight mb-6">
                            Instant API Gateway. <span className="text-fd-muted-foreground">Just add code.</span>
                        </h2>

                        <p className="text-lg text-fd-muted-foreground mb-8 leading-relaxed">
                            Turn any Forge app into a powerful API Gateway. With the <strong>FARP protocol</strong>, services automatically announce their schemas (OpenAPI, gRPC) and the gateway configures routes instantly.
                        </p>

                        <div className="grid gap-6">
                            {features.map((feature) => (
                                <div key={feature.title} className="flex gap-4">
                                    <div className="mt-1 size-10 rounded-lg bg-fd-accent flex items-center justify-center shrink-0">
                                        <feature.icon className="size-5 text-fd-foreground" />
                                    </div>
                                    <div>
                                        <h3 className="font-semibold">{feature.title}</h3>
                                        <p className="text-sm text-fd-muted-foreground mt-1">{feature.desc}</p>
                                    </div>
                                </div>
                            ))}
                        </div>
                    </motion.div>

                    <motion.div
                        initial={{ opacity: 0, x: 20 }}
                        whileInView={{ opacity: 1, x: 0 }}
                        viewport={{ once: true }}
                        transition={{ duration: 0.6, delay: 0.2 }}
                        className="relative group"
                    >
                        <div className="absolute -inset-1 bg-gradient-to-r from-purple-500/20 to-blue-500/20 rounded-2xl blur-xl opacity-50 group-hover:opacity-75 transition duration-1000" />
                        <div className="metal-shader absolute inset-0 rounded-xl opacity-20 z-10 pointer-events-none" />
                        <DiscoveryDiagram />
                    </motion.div>
                </div>
            </div>
        </section>
    );
}