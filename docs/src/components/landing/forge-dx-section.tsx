'use client';

import { motion } from 'framer-motion';
import { Container, Terminal, Zap, Box, Layers } from 'lucide-react';
import { useEffect, useState } from 'react';

const terminalSteps = [
    { text: '$ forge dev --docker', type: 'command' },
    { text: 'ℹ️  Docker mode enabled', type: 'info' },
    { text: '→  Generating dev Dockerfile...', type: 'output' },
    { text: '→  Building image forge-dev-app:latest...', type: 'output' },
    { text: '✔  Image built in 1.2s', type: 'success' },
    { text: '→  Starting container forge-dev-app...', type: 'output' },
    { text: '✔  Container running', type: 'success' },
    { text: '→  Watching for changes...', type: 'info' },
    { text: '', type: 'spacer' },
    { text: 'Detected change in main.go', type: 'warning' },
    { text: '↺  Hot reloading...', type: 'info' },
];

function TerminalDemo() {
    const [lines, setLines] = useState<typeof terminalSteps>([]);

    useEffect(() => {
        let timeout: NodeJS.Timeout;

        const runAnimation = () => {
            setLines([]);
            let i = 0;

            const nextLine = () => {
                if (i >= terminalSteps.length) {
                    timeout = setTimeout(runAnimation, 5000);
                    return;
                }

                const step = terminalSteps[i];
                if (step) {
                    setLines(prev => [...prev, step]);
                }
                i++;
                timeout = setTimeout(nextLine, i === 1 ? 800 : 400);
            };

            nextLine();
        };

        runAnimation();
        return () => clearTimeout(timeout);
    }, []);

    return (
        <div className="relative overflow-hidden border border-fd-border bg-[#0d1117] shadow-2xl shadow-black/50 rounded-lg font-mono text-xs leading-relaxed">
            <div className="flex items-center justify-between border-b border-white/10 px-4 py-2 bg-white/5">
                <div className="flex items-center gap-1.5">
                    <div className="size-2.5 rounded-full bg-red-500/70" />
                    <div className="size-2.5 rounded-full bg-yellow-500/70" />
                    <div className="size-2.5 rounded-full bg-green-500/70" />
                </div>
                <div className="text-white/30 text-[10px]">forge-dev</div>
            </div>
            <div className="p-4 h-[300px] overflow-hidden">
                {lines.map((line, idx) => (
                    <div key={idx} className={`${line.type === 'command' ? 'text-white font-bold' :
                        line.type === 'success' ? 'text-emerald-400' :
                            line.type === 'warning' ? 'text-amber-400' :
                                line.type === 'info' ? 'text-blue-400' :
                                    'text-zinc-400'
                        }`}>
                        {line.type === 'command' && <span className="text-emerald-500 mr-2">$</span>}
                        {line.text}
                    </div>
                ))}
                <div className="w-2 h-4 bg-emerald-500/50 animate-pulse mt-1" />
            </div>
        </div>
    );
}

export function ForgeDXSection() {
    return (
        <section className="container max-w-(--fd-layout-width) mx-auto px-6">
            <motion.div
                initial={{ opacity: 0, y: 32 }}
                whileInView={{ opacity: 1, y: 0 }}
                viewport={{ once: true, margin: '-80px' }}
                transition={{ duration: 0.6, ease: 'easeOut' }}
                className="group relative overflow-hidden border border-fd-border bg-fd-card"
            >
                <div className="absolute inset-0 bg-fd-card z-0" />
                <div className="metal-shader absolute inset-0 opacity-40 z-0 pointer-events-none" />
                <div className="noise-overlay absolute inset-0 pointer-events-none opacity-50" />

                <div className="relative z-10 grid lg:grid-cols-2 gap-0">
                    <div className="p-8 lg:p-12 flex flex-col justify-center order-2 lg:order-1">
                        <div className="inline-flex items-center gap-2 border border-emerald-500/30 bg-emerald-500/10 px-3 py-1 text-xs font-medium text-emerald-600 dark:text-emerald-400 mb-6 w-fit">
                            <Zap className="size-3" />
                            Developer Experience
                        </div>

                        <h2 className="text-3xl font-bold tracking-tight mb-4">
                            Zero-Config Docker
                        </h2>
                        <p className="text-fd-muted-foreground text-lg mb-8 leading-relaxed">
                            Forget writing Dockerfiles. Forge auto-generates optimized containers for your app, hot-reloads on changes, and manages networks automatically.
                        </p>

                        <div className="space-y-4">
                            {[
                                { icon: Container, label: "Automatic Containerization", desc: "No Dockerfile required. We generate one optimized for dev." },
                                { icon: Layers, label: "Live Reloading", desc: "Changes sync instantly to the running container." },
                                { icon: Box, label: "Production Parity", desc: "Develop in the same environment you deploy to." }
                            ].map((item, i) => (
                                <div key={i} className="flex gap-4">
                                    <div className="mt-1 size-8 flex items-center justify-center rounded-lg bg-emerald-500/10 border border-emerald-500/20 text-emerald-500 shrink-0">
                                        <item.icon className="size-4" />
                                    </div>
                                    <div>
                                        <h3 className="font-semibold text-sm">{item.label}</h3>
                                        <p className="text-sm text-fd-muted-foreground">{item.desc}</p>
                                    </div>
                                </div>
                            ))}
                        </div>
                    </div>

                    <div className="border-t lg:border-t-0 lg:border-l border-fd-border bg-zinc-950/50 backdrop-blur-sm p-6 lg:p-12 flex items-center justify-center order-1 lg:order-2">
                        <div className="relative w-full max-w-md transform transition-transform group-hover:scale-[1.02] duration-500">
                            <div className="absolute -inset-1 bg-gradient-to-r from-emerald-500/20 to-cyan-500/20 rounded-lg blur opacity-20 group-hover:opacity-40 transition duration-500" />
                            <TerminalDemo />
                        </div>
                    </div>
                </div>
            </motion.div>
        </section>
    );
}
